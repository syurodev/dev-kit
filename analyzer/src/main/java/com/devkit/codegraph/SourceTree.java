package com.devkit.codegraph;

import java.io.IOException;
import java.nio.file.FileVisitResult;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.SimpleFileVisitor;
import java.nio.file.attribute.BasicFileAttributes;
import java.util.ArrayList;
import java.util.List;
import java.util.Set;
import java.util.function.Predicate;

/**
 * Walks a project the way a person reads it: never into build output,
 * dependency folders or VCS metadata, and in a fixed order.
 *
 * Every walk goes through here so the skip list cannot be forgotten in one
 * of them. It was, once: a folder with no Maven or Gradle layout became its
 * own source root and was then walked with no filter at all, indexing every
 * generated copy under target/ and build/ next to the real source — and
 * those copies declare the same classes, which is exactly the duplicate that
 * takes an index down.
 *
 * Skipped directories are pruned, not filtered: a node_modules or .git is
 * never descended into, which is the difference between a walk of a large
 * checkout taking milliseconds and taking minutes. That is also why no depth
 * limit is needed; the old one (six levels) missed services nested deeper
 * without making anything faster that pruning does not already.
 */
final class SourceTree {

    /** Directories that never hold source anyone wrote. */
    static final Set<String> SKIP_DIRS = Set.of(
            ".git", ".hg", ".svn", "node_modules", "vendor",
            "target", "build", "out", "dist", ".gradle", ".idea", ".vscode");

    private SourceTree() {}

    static boolean isSkipped(String directoryName) {
        return SKIP_DIRS.contains(directoryName) || (directoryName.startsWith(".") && directoryName.length() > 1);
    }

    /** Every regular file under root matching the filter, skip list applied, sorted by path. */
    static List<Path> files(Path root, Predicate<Path> filter) throws IOException {
        List<Path> found = new ArrayList<>();
        if (!Files.isDirectory(root)) {
            return found;
        }
        Files.walkFileTree(root, new SimpleFileVisitor<>() {
            @Override
            public FileVisitResult preVisitDirectory(Path dir, BasicFileAttributes attrs) {
                if (!dir.equals(root) && isSkipped(dir.getFileName().toString())) {
                    return FileVisitResult.SKIP_SUBTREE;
                }
                return FileVisitResult.CONTINUE;
            }

            @Override
            public FileVisitResult visitFile(Path file, BasicFileAttributes attrs) {
                if (attrs.isRegularFile() && filter.test(file)) {
                    found.add(file);
                }
                return FileVisitResult.CONTINUE;
            }

            @Override
            public FileVisitResult visitFileFailed(Path file, IOException e) {
                return FileVisitResult.CONTINUE; // one unreadable entry must not end the walk
            }
        });
        found.sort(null);
        return found;
    }

    /**
     * Every src/main/java under root, sorted. A source root is not searched
     * for further source roots inside it: its files are all walked from the
     * outer one anyway.
     */
    static List<Path> conventionalSourceRoots(Path root) throws IOException {
        List<Path> roots = new ArrayList<>();
        if (!Files.isDirectory(root)) {
            return roots;
        }
        Path marker = Path.of("src", "main", "java");
        Files.walkFileTree(root, new SimpleFileVisitor<>() {
            @Override
            public FileVisitResult preVisitDirectory(Path dir, BasicFileAttributes attrs) {
                if (!dir.equals(root) && isSkipped(dir.getFileName().toString())) {
                    return FileVisitResult.SKIP_SUBTREE;
                }
                if (dir.endsWith(marker)) {
                    roots.add(dir);
                    return FileVisitResult.SKIP_SUBTREE;
                }
                return FileVisitResult.CONTINUE;
            }

            @Override
            public FileVisitResult visitFileFailed(Path file, IOException e) {
                return FileVisitResult.CONTINUE;
            }
        });
        roots.sort(null);
        return roots;
    }
}
