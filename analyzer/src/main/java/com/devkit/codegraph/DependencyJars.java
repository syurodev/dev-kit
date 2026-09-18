package com.devkit.codegraph;

import org.w3c.dom.Document;
import org.w3c.dom.Element;
import org.w3c.dom.Node;

import javax.xml.XMLConstants;
import javax.xml.parsers.DocumentBuilder;
import javax.xml.parsers.DocumentBuilderFactory;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.Deque;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Stream;

/**
 * The jars in the local Maven and Gradle caches that this project actually
 * depends on, read from its build files without running Maven or Gradle
 * (NFR-1).
 *
 * The analyzer used to hand the symbol solver every jar in ~/.m2 and the
 * Gradle cache. On a developer machine that is tens of thousands of jars: a
 * crawl that took minutes and a heap that took gigabytes, to resolve calls
 * into a handful of libraries. Now the coordinates come from the build files
 * — pom.xml dependencies, and the 'group:artifact:version' strings Gradle
 * scripts and version catalogs are written with — and each is looked up at
 * its one cache path.
 *
 * Direct dependencies are rarely where the types live: a Spring Boot starter
 * is an empty jar whose pom pulls in spring-web and friends. So the cached
 * .pom of each dependency is read too, following compile and runtime
 * dependencies a few levels down. Both walks are bounded (MAX_DEPTH,
 * MAX_JARS), because the point is to stay small.
 *
 * Everything here is best-effort. A version this cannot work out — a
 * property defined in a parent pom, a BOM-managed version Gradle never
 * writes down — falls back to the newest version present in the cache,
 * which is almost always the one the build resolved or close enough for
 * symbol resolution. A dependency not in the cache is simply not added.
 *
 * With no build file at all, or none naming anything, there is nothing to
 * go on, and a full-cache scan is exactly what this replaced. The fallback
 * is instead a short fixed list (FALLBACK) of the libraries a Spring
 * codebase calls most, at their newest cached version. Most of the graph
 * does not need jars anyway: layers and routes are matched by annotation
 * name, and project-to-project calls resolve from source.
 *
 * The result is ordered deterministically: build files in path order,
 * dependencies in the order they are declared, breadth first.
 */
final class DependencyJars {

    static final int MAX_JARS = 400;
    static final int MAX_DEPTH = 3;

    /** group, artifact, and version (null: the newest one cached). */
    record Coordinate(String group, String artifact, String version) {
        String key() {
            return group + ":" + artifact;
        }
    }

    static final List<Coordinate> FALLBACK = List.of(
            new Coordinate("org.springframework", "spring-web", null),
            new Coordinate("org.springframework", "spring-webmvc", null),
            new Coordinate("org.springframework", "spring-context", null),
            new Coordinate("org.springframework", "spring-beans", null),
            new Coordinate("org.springframework", "spring-core", null),
            new Coordinate("org.springframework", "spring-tx", null),
            new Coordinate("org.springframework.data", "spring-data-commons", null),
            new Coordinate("org.springframework.data", "spring-data-jpa", null),
            new Coordinate("jakarta.persistence", "jakarta.persistence-api", null),
            new Coordinate("org.slf4j", "slf4j-api", null));

    private static final Set<String> BUILD_FILES = Set.of(
            "pom.xml", "build.gradle", "build.gradle.kts", "libs.versions.toml");

    // 'group:artifact' or 'group:artifact:version', optionally ':classifier', quoted.
    private static final Pattern GRADLE_COORDINATE = Pattern.compile(
            "[\"']([A-Za-z0-9_.\\-]+):([A-Za-z0-9_.\\-]+)(?::([^:\"'\\s]+))?(?::[^\"'\\s]*)?[\"']");
    // group: 'g', name: 'a', version: 'v' (Groovy map notation)
    private static final Pattern GRADLE_MAP = Pattern.compile(
            "group\\s*[:=]\\s*[\"']([^\"']+)[\"']\\s*,\\s*name\\s*[:=]\\s*[\"']([^\"']+)[\"']"
                    + "(?:\\s*,\\s*version\\s*[:=]\\s*[\"']([^\"']+)[\"'])?");

    private final Path mavenRepository;
    private final Path gradleFiles;

    private DependencyJars(Path mavenRepository, Path gradleFiles) {
        this.mavenRepository = mavenRepository;
        this.gradleFiles = gradleFiles;
    }

    /** The default caches: ~/.m2/repository and ~/.gradle/caches/modules-2/files-2.1. */
    static List<Path> find(Path projectRoot) {
        Path home = Path.of(System.getProperty("user.home"));
        return find(projectRoot,
                home.resolve(".m2").resolve("repository"),
                home.resolve(".gradle").resolve("caches").resolve("modules-2").resolve("files-2.1"));
    }

    static List<Path> find(Path projectRoot, Path mavenRepository, Path gradleFiles) {
        DependencyJars finder = new DependencyJars(mavenRepository, gradleFiles);
        List<Coordinate> direct = projectRoot == null ? List.of() : declaredCoordinates(projectRoot);
        return finder.resolve(direct.isEmpty() ? FALLBACK : direct);
    }

    /** Every dependency the project's build files name, in file then declaration order. */
    static List<Coordinate> declaredCoordinates(Path projectRoot) {
        List<Path> buildFiles;
        try {
            buildFiles = SourceTree.files(projectRoot, p -> BUILD_FILES.contains(p.getFileName().toString()));
        } catch (IOException e) {
            return List.of();
        }
        List<Coordinate> coordinates = new ArrayList<>();
        for (Path file : buildFiles) {
            String name = file.getFileName().toString();
            if (name.equals("pom.xml")) {
                readPom(file).ifPresent(pom -> coordinates.addAll(pom.dependencies(true)));
            } else {
                coordinates.addAll(readGradle(file));
            }
        }
        return coordinates;
    }

    private List<Path> resolve(List<Coordinate> direct) {
        Set<Path> jars = new LinkedHashSet<>();
        Set<String> visited = new HashSet<>();
        Deque<Map.Entry<Coordinate, Integer>> queue = new ArrayDeque<>();
        for (Coordinate c : direct) {
            queue.add(Map.entry(c, 0));
        }
        while (!queue.isEmpty() && jars.size() < MAX_JARS) {
            var next = queue.poll();
            Coordinate c = next.getKey();
            // Nearest declaration wins, as in Maven's own mediation.
            if (!visited.add(c.key())) {
                continue;
            }
            Optional<String> version = version(c);
            if (version.isEmpty()) {
                continue; // not in either cache
            }
            artifactFile(c, version.get(), ".jar").ifPresent(jars::add);
            if (next.getValue() < MAX_DEPTH) {
                artifactFile(c, version.get(), ".pom")
                        .flatMap(DependencyJars::readPom)
                        .ifPresent(pom -> {
                            for (Coordinate child : pom.dependencies(false)) {
                                queue.add(Map.entry(child, next.getValue() + 1));
                            }
                        });
            }
        }
        return new ArrayList<>(jars);
    }

    /** The declared version when it is cached, else the newest cached one. */
    private Optional<String> version(Coordinate c) {
        List<String> cached = new ArrayList<>();
        for (Path dir : List.of(mavenArtifactDir(c), gradleArtifactDir(c))) {
            if (!Files.isDirectory(dir)) {
                continue;
            }
            try (Stream<Path> versions = Files.list(dir)) {
                versions.filter(Files::isDirectory)
                        .map(p -> p.getFileName().toString())
                        .forEach(cached::add);
            } catch (IOException ignored) {
                // unreadable cache directory: treat as absent
            }
        }
        if (cached.isEmpty()) {
            return Optional.empty();
        }
        if (c.version() != null && cached.contains(c.version())) {
            return Optional.of(c.version());
        }
        return cached.stream().max(VERSION_ORDER);
    }

    private Optional<Path> artifactFile(Coordinate c, String version, String extension) {
        String fileName = c.artifact() + "-" + version + extension;
        Path maven = mavenArtifactDir(c).resolve(version).resolve(fileName);
        if (Files.isRegularFile(maven)) {
            return Optional.of(maven);
        }
        // Gradle keeps each file under a directory named for its hash.
        Path gradle = gradleArtifactDir(c).resolve(version);
        if (Files.isDirectory(gradle)) {
            try (Stream<Path> hashes = Files.list(gradle)) {
                return hashes.sorted()
                        .map(h -> h.resolve(fileName))
                        .filter(Files::isRegularFile)
                        .findFirst();
            } catch (IOException ignored) {
                return Optional.empty();
            }
        }
        return Optional.empty();
    }

    private Path mavenArtifactDir(Coordinate c) {
        Path dir = mavenRepository;
        for (String part : c.group().split("\\.")) {
            dir = dir.resolve(part);
        }
        return dir.resolve(c.artifact());
    }

    private Path gradleArtifactDir(Coordinate c) {
        return gradleFiles.resolve(c.group()).resolve(c.artifact());
    }

    // --- Gradle ---------------------------------------------------------------

    static List<Coordinate> readGradle(Path file) {
        List<String> lines;
        try {
            lines = Files.readAllLines(file);
        } catch (IOException e) {
            return List.of();
        }
        List<Coordinate> coordinates = new ArrayList<>();
        for (String line : lines) {
            String trimmed = line.trim();
            // Test-only configurations: nothing under src/main uses them.
            if (trimmed.startsWith("test") || trimmed.startsWith("androidTest") || trimmed.startsWith("//")) {
                continue;
            }
            Matcher map = GRADLE_MAP.matcher(line);
            while (map.find()) {
                coordinates.add(new Coordinate(map.group(1), map.group(2), literalVersion(map.group(3))));
            }
            Matcher m = GRADLE_COORDINATE.matcher(line);
            while (m.find()) {
                coordinates.add(new Coordinate(m.group(1), m.group(2), literalVersion(m.group(3))));
            }
        }
        return coordinates;
    }

    /** A version Gradle interpolates ("$springVersion") or ranges ("1.+") is not a directory name. */
    private static String literalVersion(String version) {
        if (version == null || version.contains("$") || version.contains("+") || version.contains("[")) {
            return null;
        }
        return version;
    }

    // --- Maven ----------------------------------------------------------------

    private record Pom(List<Element> dependencies, Map<String, String> properties, Map<String, String> managed) {
        List<Coordinate> dependencies(boolean projectLevel) {
            List<Coordinate> out = new ArrayList<>();
            for (Element d : dependencies) {
                String group = interpolate(text(d, "groupId"));
                String artifact = interpolate(text(d, "artifactId"));
                if (group == null || artifact == null) {
                    continue;
                }
                String scope = Optional.ofNullable(text(d, "scope")).orElse("compile");
                String type = Optional.ofNullable(text(d, "type")).orElse("jar");
                boolean optional = "true".equals(text(d, "optional"));
                if (scope.equals("test") || scope.equals("import") || !type.equals("jar")) {
                    continue;
                }
                // A dependency's own provided/optional dependencies never
                // reach the project's classpath; the project's own do.
                if (!projectLevel && (optional || scope.equals("provided") || scope.equals("system"))) {
                    continue;
                }
                String version = interpolate(text(d, "version"));
                if (version == null) {
                    version = managed.get(group + ":" + artifact);
                }
                out.add(new Coordinate(group, artifact, version));
            }
            return out;
        }

        /** The value with ${...} replaced from this pom's properties, or null if one is not defined here. */
        String interpolate(String value) {
            if (value == null) {
                return null;
            }
            String result = value;
            for (int i = 0; i < 5 && result.contains("${"); i++) {
                Matcher m = Pattern.compile("\\$\\{([^}]+)}").matcher(result);
                StringBuilder sb = new StringBuilder();
                while (m.find()) {
                    String replacement = properties.get(m.group(1));
                    if (replacement == null) {
                        return null;
                    }
                    m.appendReplacement(sb, Matcher.quoteReplacement(replacement));
                }
                m.appendTail(sb);
                result = sb.toString();
            }
            return result.contains("${") ? null : result.trim();
        }
    }

    private static Optional<Pom> readPom(Path file) {
        Document doc;
        try {
            DocumentBuilderFactory factory = DocumentBuilderFactory.newInstance();
            // A pom is data: no DTDs, no external entities.
            factory.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
            factory.setAttribute(XMLConstants.ACCESS_EXTERNAL_DTD, "");
            factory.setAttribute(XMLConstants.ACCESS_EXTERNAL_SCHEMA, "");
            factory.setExpandEntityReferences(false);
            DocumentBuilder builder = factory.newDocumentBuilder();
            builder.setErrorHandler(null);
            doc = builder.parse(file.toFile());
        } catch (Exception e) {
            return Optional.empty(); // malformed or unreadable: one pom, not the analysis
        }
        Element project = doc.getDocumentElement();

        Map<String, String> properties = new HashMap<>();
        Element props = child(project, "properties");
        if (props != null) {
            for (Element p : children(props)) {
                properties.put(p.getTagName(), p.getTextContent().trim());
            }
        }
        Element parent = child(project, "parent");
        String parentVersion = parent == null ? null : text(parent, "version");
        String version = Optional.ofNullable(text(project, "version")).orElse(parentVersion);
        String groupId = Optional.ofNullable(text(project, "groupId"))
                .orElse(parent == null ? null : text(parent, "groupId"));
        if (version != null) {
            properties.put("project.version", version);
            properties.put("version", version);
        }
        if (groupId != null) {
            properties.put("project.groupId", groupId);
        }
        if (parentVersion != null) {
            properties.put("project.parent.version", parentVersion);
        }

        List<Element> dependencies = new ArrayList<>();
        Element deps = child(project, "dependencies");
        if (deps != null) {
            dependencies.addAll(children(deps, "dependency"));
        }
        Pom partial = new Pom(dependencies, properties, new HashMap<>());
        Element management = child(project, "dependencyManagement");
        Element managedDeps = management == null ? null : child(management, "dependencies");
        if (managedDeps != null) {
            for (Element d : children(managedDeps, "dependency")) {
                String g = partial.interpolate(text(d, "groupId"));
                String a = partial.interpolate(text(d, "artifactId"));
                String v = partial.interpolate(text(d, "version"));
                if (g != null && a != null && v != null) {
                    partial.managed().putIfAbsent(g + ":" + a, v);
                }
            }
        }
        return Optional.of(partial);
    }

    private static Element child(Element parent, String name) {
        for (Element e : children(parent)) {
            if (e.getTagName().equals(name)) {
                return e;
            }
        }
        return null;
    }

    private static List<Element> children(Element parent, String name) {
        return children(parent).stream().filter(e -> e.getTagName().equals(name)).toList();
    }

    private static List<Element> children(Element parent) {
        List<Element> out = new ArrayList<>();
        for (Node n = parent.getFirstChild(); n != null; n = n.getNextSibling()) {
            if (n instanceof Element e) {
                out.add(e);
            }
        }
        return out;
    }

    private static String text(Element parent, String name) {
        Element e = child(parent, name);
        if (e == null) {
            return null;
        }
        String value = e.getTextContent().trim();
        return value.isEmpty() ? null : value;
    }

    // --- Versions -------------------------------------------------------------

    /**
     * Numeric segments compared as numbers, so 6.10 is newer than 6.9; on a
     * tie a plain release is newer than one with a qualifier (6.1.0 over
     * 6.1.0-RC1); otherwise qualifiers compare as text. Total and stable, which
     * is what makes the pick deterministic.
     */
    static final Comparator<String> VERSION_ORDER = (a, b) -> {
        String[] pa = a.split("[.\\-]");
        String[] pb = b.split("[.\\-]");
        for (int i = 0; i < Math.min(pa.length, pb.length); i++) {
            boolean na = pa[i].matches("\\d+");
            boolean nb = pb[i].matches("\\d+");
            int cmp;
            if (na && nb) {
                cmp = new java.math.BigInteger(pa[i]).compareTo(new java.math.BigInteger(pb[i]));
            } else if (na != nb) {
                cmp = na ? 1 : -1;
            } else {
                cmp = pa[i].compareToIgnoreCase(pb[i]);
            }
            if (cmp != 0) {
                return cmp;
            }
        }
        if (pa.length != pb.length) {
            // The longer one carries an extra segment: numeric means newer
            // (1.0.1 > 1.0), a qualifier means older (1.0-RC1 < 1.0).
            String[] longer = pa.length > pb.length ? pa : pb;
            boolean extraIsNumeric = longer[Math.min(pa.length, pb.length)].matches("\\d+");
            int sign = pa.length > pb.length ? 1 : -1;
            return extraIsNumeric ? sign : -sign;
        }
        return a.compareTo(b);
    };
}
