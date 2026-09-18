package com.devkit.codegraph;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.IOException;
import java.io.OutputStream;
import java.nio.file.AtomicMoveNotSupportedException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;

/**
 * Writes the index JSON so a reader never sees half of it.
 *
 * The JSON is streamed to a temporary file beside the target — same
 * directory, so the same filesystem — and moved into place only once it is
 * complete. Building the whole document as one String first, as this used
 * to, held a second copy of a large index in memory; and a crash mid-write
 * left a truncated file where the caller expected an index.
 */
public class AnalysisResultWriter {
    private static final ObjectMapper MAPPER = new ObjectMapper();

    public static void write(Path outputPath, AnalysisResult result) throws IOException {
        Path target = outputPath.toAbsolutePath();
        Path dir = target.getParent();
        Path temp = Files.createTempFile(dir, target.getFileName().toString(), ".tmp");
        try {
            try (OutputStream out = Files.newOutputStream(temp)) {
                MAPPER.writeValue(out, result);
            }
            try {
                Files.move(temp, target, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING);
            } catch (AtomicMoveNotSupportedException e) {
                Files.move(temp, target, StandardCopyOption.REPLACE_EXISTING);
            }
        } finally {
            Files.deleteIfExists(temp);
        }
    }
}
