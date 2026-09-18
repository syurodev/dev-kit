package com.devkit.codegraph;

import com.github.javaparser.symbolsolver.resolution.typesolvers.CombinedTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.JarTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.JavaParserTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.ReflectionTypeSolver;

import java.io.IOException;
import java.nio.file.Path;
import java.util.List;

/**
 * Builds a best-effort CombinedTypeSolver: the JDK (reflection), the
 * project's own source (so cross-file resolution works), and the jars from
 * the user's local Maven/Gradle caches that the project's build files name
 * (see DependencyJars — not the whole cache, which is tens of thousands of
 * jars). This deliberately never runs `mvn`/`gradle` to compute the real
 * dependency tree (NFR-1) — some calls into third-party libraries the
 * project doesn't happen to have locally cached will be unresolved, and
 * that's an accepted v1 limitation, not a bug (see codegraph.mdx's Known
 * ceiling).
 */
public class TypeSolverFactory {

    /** One source root and no jars: the JDK and the source alone. */
    public static CombinedTypeSolver build(Path projectSourceRoot) throws IOException {
        return build(null, List.of(projectSourceRoot));
    }

    /**
     * One solver over every source root in the project.
     *
     * A repository of microservices has one root per service, and they belong
     * in the same solver rather than in one solver each: that is what lets a
     * call from one service into a shared module resolve to a real declaration
     * instead of going unresolved.
     *
     * projectRoot is where the build files are read from; null adds no jars.
     */
    public static CombinedTypeSolver build(Path projectRoot, List<Path> projectSourceRoots) throws IOException {
        CombinedTypeSolver typeSolver = new CombinedTypeSolver();
        typeSolver.add(new ReflectionTypeSolver());
        for (Path sourceRoot : projectSourceRoots) {
            typeSolver.add(new JavaParserTypeSolver(sourceRoot));
        }
        if (projectRoot == null) {
            return typeSolver;
        }
        for (Path jar : DependencyJars.find(projectRoot)) {
            try {
                typeSolver.add(new JarTypeSolver(jar));
            } catch (IOException | RuntimeException e) {
                // A single unreadable/corrupt jar must not abort the whole
                // analysis — skip it and keep going.
            }
        }
        return typeSolver;
    }
}
