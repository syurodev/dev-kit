package com.devkit.codegraph;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

public class AnalysisResultWriter {
    private static final ObjectMapper MAPPER = new ObjectMapper();

    public static void write(Path outputPath, AnalysisResult result) throws IOException {
        Files.writeString(outputPath, MAPPER.writeValueAsString(result));
    }
}
