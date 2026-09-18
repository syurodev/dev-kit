package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

class DependencyJarsTest {

    private static Path mavenArtifact(Path repo, String group, String artifact, String version, String pom)
            throws IOException {
        Path dir = repo.resolve(group.replace('.', '/')).resolve(artifact).resolve(version);
        Files.createDirectories(dir);
        Path jar = dir.resolve(artifact + "-" + version + ".jar");
        Files.writeString(jar, "");
        Files.writeString(dir.resolve(artifact + "-" + version + "-sources.jar"), "");
        if (pom != null) {
            Files.writeString(dir.resolve(artifact + "-" + version + ".pom"), pom);
        }
        return jar;
    }

    private static Path gradleArtifact(Path files, String group, String artifact, String version) throws IOException {
        Path dir = files.resolve(group).resolve(artifact).resolve(version).resolve("0a1b2c");
        Files.createDirectories(dir);
        Path jar = dir.resolve(artifact + "-" + version + ".jar");
        Files.writeString(jar, "");
        return jar;
    }

    @Test
    void takesOnlyTheJarsThePomNamesAndWhatTheyPullIn(@TempDir Path tmp) throws IOException {
        // The old behaviour was every jar in the cache; the unrelated one
        // here stands for the tens of thousands of those.
        Path repo = tmp.resolve("m2");
        Path gradle = tmp.resolve("gradle");
        Path starter = mavenArtifact(repo, "org.springframework.boot", "spring-boot-starter-web", "3.3.0", """
                <project>
                  <dependencies>
                    <dependency><groupId>org.springframework</groupId><artifactId>spring-web</artifactId><version>${spring.version}</version></dependency>
                    <dependency><groupId>org.example</groupId><artifactId>optional-thing</artifactId><version>1.0</version><optional>true</optional></dependency>
                  </dependencies>
                </project>
                """);
        mavenArtifact(repo, "org.springframework", "spring-web", "6.0.9", null);
        Path springWeb = mavenArtifact(repo, "org.springframework", "spring-web", "6.1.10", null);
        mavenArtifact(repo, "org.springframework", "spring-web", "6.1.10-RC1", null);
        Path guava = mavenArtifact(repo, "com.google.guava", "guava", "33.0.0-jre", null);
        mavenArtifact(repo, "com.google.guava", "guava", "32.0.0-jre", null);
        mavenArtifact(repo, "org.example", "optional-thing", "1.0", null);
        mavenArtifact(repo, "org.example", "unrelated", "1.0", null);
        mavenArtifact(repo, "org.junit.jupiter", "junit-jupiter", "5.11.0", null);

        Path project = tmp.resolve("project");
        Files.createDirectories(project);
        Files.writeString(project.resolve("pom.xml"), """
                <project>
                  <properties><guava.version>33.0.0-jre</guava.version></properties>
                  <dependencies>
                    <dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency>
                    <dependency><groupId>com.google.guava</groupId><artifactId>guava</artifactId><version>${guava.version}</version></dependency>
                    <dependency><groupId>org.junit.jupiter</groupId><artifactId>junit-jupiter</artifactId><version>5.11.0</version><scope>test</scope></dependency>
                  </dependencies>
                </project>
                """);

        List<Path> jars = DependencyJars.find(project, repo, gradle);

        // The starter's version is managed by a parent this pom does not
        // have and spring.version is undefined here, so both fall back to
        // the newest release cached; guava's property resolves exactly.
        assertEquals(List.of(starter, guava, springWeb), jars);
    }

    @Test
    void readsGradleCoordinatesAndTheGradleCache(@TempDir Path tmp) throws IOException {
        Path repo = tmp.resolve("m2");
        Path gradle = tmp.resolve("gradle");
        Path lombok = gradleArtifact(gradle, "org.projectlombok", "lombok", "1.18.32");
        Path jackson = gradleArtifact(gradle, "com.fasterxml.jackson.core", "jackson-databind", "2.17.1");
        gradleArtifact(gradle, "com.fasterxml.jackson.core", "jackson-databind", "2.16.0");
        gradleArtifact(gradle, "org.mockito", "mockito-core", "5.0.0");

        Path project = tmp.resolve("project");
        Files.createDirectories(project.resolve("service"));
        Files.writeString(project.resolve("service/build.gradle.kts"), """
                dependencies {
                    compileOnly("org.projectlombok:lombok:1.18.32")
                    implementation("com.fasterxml.jackson.core:jackson-databind")
                    testImplementation("org.mockito:mockito-core:5.0.0")
                }
                """);
        // Build output is never read for coordinates.
        Files.createDirectories(project.resolve("build"));
        Files.writeString(project.resolve("build/build.gradle"), "implementation 'org.mockito:mockito-core:5.0.0'");

        assertEquals(List.of(lombok, jackson), DependencyJars.find(project, repo, gradle));
    }

    @Test
    void withNoBuildFileFallsBackToAShortFixedListNotTheWholeCache(@TempDir Path tmp) throws IOException {
        Path repo = tmp.resolve("m2");
        Path springWeb = mavenArtifact(repo, "org.springframework", "spring-web", "6.1.10", null);
        mavenArtifact(repo, "org.example", "unrelated", "1.0", null);
        Path project = tmp.resolve("project");
        Files.createDirectories(project);

        assertEquals(List.of(springWeb), DependencyJars.find(project, repo, tmp.resolve("gradle")));
    }

    @Test
    void ordersVersionsNumericallyWithReleasesAfterTheirPreReleases() {
        List<String> sorted = List.of("6.10.0", "6.9.1", "6.10.0-RC1", "6.2", "6.10.0.1").stream()
                .sorted(DependencyJars.VERSION_ORDER).toList();
        assertEquals(List.of("6.2", "6.9.1", "6.10.0-RC1", "6.10.0", "6.10.0.1"), sorted);
    }
}
