package com.devkit.codegraph;

import org.yaml.snakeyaml.LoaderOptions;
import org.yaml.snakeyaml.Yaml;
import org.yaml.snakeyaml.nodes.MappingNode;
import org.yaml.snakeyaml.nodes.Node;
import org.yaml.snakeyaml.nodes.NodeTuple;
import org.yaml.snakeyaml.nodes.ScalarNode;
import org.yaml.snakeyaml.nodes.SequenceNode;

import java.io.IOException;
import java.io.Reader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/**
 * Reads the Spring config files of a project — {@code application*.yml},
 * {@code application*.yaml}, {@code application*.properties} and the
 * {@code bootstrap} equivalents — into flat {@link ConfigKey}s.
 *
 * Text, not evaluation. YAML is composed into a node tree (never constructed
 * into objects, so a tag cannot instantiate anything) and flattened with the
 * line each key was written on. Nothing is merged across files or profiles:
 * every entry stays where it was written, tagged with the profile it applies
 * to, because which one wins is a runtime question.
 */
final class ConfigFiles {

    private static final Pattern FILE_NAME =
            Pattern.compile("^(application|bootstrap)(-(.+))?\\.(ya?ml|properties)$");

    /** A real config file has hundreds of keys; this only stops an alias bomb or a data file from filling the index. */
    static final int MAX_KEYS_PER_FILE = 5000;
    static final int MAX_VALUE_LENGTH = 200;
    private static final int MAX_DEPTH = 24;

    /** Keys whose values are secrets: kept as a key, never as a value. Compared with separators removed. */
    private static final Pattern SECRET = Pattern.compile(
            "password|passwd|pwd|secret|token|credential|apikey|privatekey|accesskey|signingkey|encryptionkey|passphrase");
    private static final Pattern PLACEHOLDER = Pattern.compile("\\$\\{([A-Za-z_][A-Za-z0-9_]*)(?::[^}]*)?}");
    private static final Pattern ENV_NAME = Pattern.compile("[A-Z][A-Z0-9_]*");

    private ConfigFiles() {}

    /** Every config file under the project, test resources excluded, in path order. */
    static List<ConfigKey> read(Path projectRoot) {
        List<ConfigKey> keys = new ArrayList<>();
        List<Path> files;
        try {
            files = SourceTree.files(projectRoot, ConfigFiles::isConfigFile);
        } catch (IOException e) {
            return keys;
        }
        for (Path file : files) {
            String relative = projectRoot.relativize(file).toString().replace('\\', '/');
            Matcher name = FILE_NAME.matcher(file.getFileName().toString());
            if (!name.matches()) {
                continue;
            }
            String fileProfile = name.group(3) == null ? "" : name.group(3);
            try {
                if (name.group(4).equals("properties")) {
                    readProperties(file, relative, fileProfile, keys);
                } else {
                    readYaml(file, relative, fileProfile, keys);
                }
            } catch (IOException | RuntimeException e) {
                // A file that does not parse is skipped whole, like an unparseable source file:
                // one bad file must not take the rest of the index with it.
            }
        }
        return keys;
    }

    private static boolean isConfigFile(Path file) {
        if (!FILE_NAME.matcher(file.getFileName().toString()).matches()) {
            return false;
        }
        String path = file.toString().replace('\\', '/');
        return !path.contains("/src/test/");
    }

    // --- YAML ------------------------------------------------------------------------------

    private static void readYaml(Path file, String relative, String fileProfile, List<ConfigKey> out) throws IOException {
        LoaderOptions options = new LoaderOptions();
        options.setMaxAliasesForCollections(50);
        options.setCodePointLimit(4 * 1024 * 1024);
        Yaml yaml = new Yaml(options);
        int budget = MAX_KEYS_PER_FILE;
        try (Reader reader = Files.newBufferedReader(file, StandardCharsets.UTF_8)) {
            for (Node document : yaml.composeAll(reader)) {
                List<ConfigKey> entries = new ArrayList<>();
                flatten(document, "", relative, entries, 0, budget);
                budget -= entries.size();
                // A multi-document file says which profile a document is for.
                String profile = fileProfile;
                for (ConfigKey e : entries) {
                    if (e.key.equals("spring.config.activate.on-profile") || e.key.equals("spring.profiles")) {
                        profile = e.value;
                    }
                }
                for (ConfigKey e : entries) {
                    e.profile = profile;
                    out.add(e);
                }
                if (budget <= 0) {
                    return;
                }
            }
        }
    }

    private static void flatten(Node node, String path, String file, List<ConfigKey> out, int depth, int budget) {
        if (node == null || depth > MAX_DEPTH || out.size() >= budget) {
            return;
        }
        if (node instanceof MappingNode mapping) {
            for (NodeTuple tuple : mapping.getValue()) {
                if (!(tuple.getKeyNode() instanceof ScalarNode keyNode)) {
                    continue;
                }
                String key = keyNode.getValue();
                if (key.equals("<<")) {
                    continue; // a merge key: the entries it pulls in are written elsewhere
                }
                flatten(tuple.getValueNode(), path.isEmpty() ? key : path + "." + key, file, out, depth + 1, budget);
                if (out.size() >= budget) {
                    return;
                }
            }
        } else if (node instanceof SequenceNode sequence) {
            int index = 0;
            for (Node item : sequence.getValue()) {
                flatten(item, path + "[" + index++ + "]", file, out, depth + 1, budget);
                if (out.size() >= budget) {
                    return;
                }
            }
        } else if (node instanceof ScalarNode scalar && !path.isEmpty()) {
            out.add(entry(path, scalar.getValue(), file, scalar.getStartMark().getLine() + 1));
        }
    }

    // --- .properties ------------------------------------------------------------------------

    private static void readProperties(Path file, String relative, String profile, List<ConfigKey> out) throws IOException {
        List<String> lines = Files.readAllLines(file, StandardCharsets.UTF_8);
        for (int i = 0; i < lines.size() && out.size() < MAX_KEYS_PER_FILE; i++) {
            String line = lines.get(i).strip();
            if (line.isEmpty() || line.startsWith("#") || line.startsWith("!")) {
                continue;
            }
            int first = i + 1;
            // A trailing backslash continues the entry on the next line.
            StringBuilder logical = new StringBuilder(line);
            while (endsWithContinuation(logical) && i + 1 < lines.size()) {
                logical.setLength(logical.length() - 1);
                logical.append(lines.get(++i).strip());
            }
            int split = separator(logical);
            String key = (split < 0 ? logical.toString() : logical.substring(0, split)).strip();
            String value = split < 0 ? "" : logical.substring(split + 1).strip();
            if (!key.isEmpty()) {
                ConfigKey e = entry(key, value, relative, first);
                e.profile = profile;
                out.add(e);
            }
        }
    }

    private static boolean endsWithContinuation(CharSequence s) {
        int backslashes = 0;
        for (int i = s.length() - 1; i >= 0 && s.charAt(i) == '\\'; i--) {
            backslashes++;
        }
        return backslashes % 2 == 1;
    }

    /** The index of the first unescaped '=' or ':' — or of whitespace, which also separates in a properties file. */
    private static int separator(CharSequence s) {
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == '\\') {
                i++;
            } else if (c == '=' || c == ':') {
                return i;
            } else if (c == ' ' || c == '\t') {
                int j = i;
                while (j < s.length() && (s.charAt(j) == ' ' || s.charAt(j) == '\t')) {
                    j++;
                }
                return j < s.length() && (s.charAt(j) == '=' || s.charAt(j) == ':') ? j : i;
            }
        }
        return -1;
    }

    // --- shared -----------------------------------------------------------------------------

    private static ConfigKey entry(String key, String value, String file, int line) {
        ConfigKey e = new ConfigKey();
        e.key = key;
        e.file = file;
        e.line = line;
        boolean secret = isSecret(key);
        e.redacted = secret;
        if (!secret) {
            e.value = value.length() <= MAX_VALUE_LENGTH ? value : value.substring(0, MAX_VALUE_LENGTH - 1) + "…";
        }
        // The names of the variables are kept even for a secret: a name is not
        // the secret, and it is how someone holding the variable finds the key.
        Matcher m = PLACEHOLDER.matcher(value);
        while (m.find()) {
            if (ENV_NAME.matcher(m.group(1)).matches() && !e.envVars.contains(m.group(1))) {
                e.envVars.add(m.group(1));
            }
        }
        return e;
    }

    /** True for a key that names a secret — separators and case ignored, so {@code db.Pass-Word} counts. */
    static boolean isSecret(String key) {
        return SECRET.matcher(key.toLowerCase(Locale.ROOT).replaceAll("[^a-z0-9]", "")).find();
    }
}
