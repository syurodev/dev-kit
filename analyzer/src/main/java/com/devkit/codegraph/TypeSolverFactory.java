package com.devkit.codegraph;

import com.github.javaparser.symbolsolver.resolution.typesolvers.CombinedTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.JarTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.JavaParserTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.ReflectionTypeSolver;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Stream;

/**
 * Builds a best-effort CombinedTypeSolver: the JDK (reflection), the
 * project's own source (so cross-file resolution works), and every .jar
 * found under the user's local Maven/Gradle caches. This deliberately never
 * runs `mvn`/`gradle` to compute the real dependency tree (NFR-1) — some
 * calls into third-party libraries the project doesn't happen to have
 * locally cached will be unresolved, and that's an accepted v1 limitation,
 * not a bug (see codegraph.mdx's Known ceiling).
 */
public class TypeSolverFactory {

    public static CombinedTypeSolver build(Path projectSourceRoot) throws IOException {
        return build(List.of(projectSourceRoot));
    }

    /**
     * One solver over every source root in the project.
     *
     * A repository of microservices has one root per service, and they belong
     * in the same solver rather than in one solver each: that is what lets a
     * call from one service into a shared module resolve to a real declaration
     * instead of going unresolved.
     */
    public static CombinedTypeSolver build(List<Path> projectSourceRoots) throws IOException {
        CombinedTypeSolver typeSolver = new CombinedTypeSolver();
        typeSolver.add(new ReflectionTypeSolver());
        for (Path sourceRoot : projectSourceRoots) {
            typeSolver.add(new JavaParserTypeSolver(sourceRoot));
        }

        addJarsUnder(typeSolver, Path.of(System.getProperty("user.home"), ".m2", "repository"));
        addJarsUnder(typeSolver, Path.of(System.getProperty("user.home"), ".gradle", "caches", "modules-2", "files-2.1"));

        return typeSolver;
    }

    private static void addJarsUnder(CombinedTypeSolver typeSolver, Path root) throws IOException {
        if (!Files.isDirectory(root)) {
            return; // no local Maven/Gradle cache at this path — fine, just fewer resolvable jars
        }
        try (Stream<Path> walk = Files.walk(root)) {
            walk.filter(p -> p.toString().endsWith(".jar"))
                .forEach(jar -> {
                    try {
                        typeSolver.add(new JarTypeSolver(jar));
                    } catch (IOException e) {
                        // A single unreadable/corrupt jar in a huge local cache must not
                        // abort the whole analysis — skip it and keep going.
                    }
                });
        }
    }
}
