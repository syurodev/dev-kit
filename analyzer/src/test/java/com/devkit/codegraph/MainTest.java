package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

import static org.junit.jupiter.api.Assertions.*;

class MainTest {

    @Test
    void writesEmptyIndexForProjectWithNoJavaFiles(@TempDir Path tempDir) throws IOException {
        Path projectDir = tempDir.resolve("project");
        Files.createDirectories(projectDir);
        Path outputFile = tempDir.resolve("index.json");

        Main.run(projectDir.toString(), outputFile.toString());

        assertTrue(Files.exists(outputFile), "index.json should be created");
        String content = Files.readString(outputFile);
        assertTrue(content.contains("\"nodes\""), "output should have a nodes field: " + content);
        assertTrue(content.contains("\"edges\""), "output should have an edges field: " + content);
        assertTrue(content.contains("\"endpoints\""), "output should have an endpoints field: " + content);
    }

    @Test
    void nonExistentProjectPathThrowsException(@TempDir Path tempDir) {
        Path nonExistent = tempDir.resolve("does-not-exist-folder-1234");
        Path outputFile = tempDir.resolve("index.json");

        assertThrows(IllegalArgumentException.class, () -> Main.run(nonExistent.toString(), outputFile.toString()));
    }
}
