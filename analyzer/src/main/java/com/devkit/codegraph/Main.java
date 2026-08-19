package com.devkit.codegraph;

import java.io.IOException;
import java.nio.file.Path;

/**
 * CLI entrypoint. Usage: java -jar codegraph-analyzer.jar <projectPath> <outputPath>
 */
public class Main {

    public static void main(String[] args) {
        if (args.length != 2) {
            System.err.println("Usage: codegraph-analyzer <projectPath> <outputPath>");
            System.exit(2);
        }
        try {
            run(args[0], args[1]);
        } catch (Exception e) {
            System.err.println("Error analyzing project: " + e.getMessage());
            System.exit(1);
        }
    }

    public static void run(String projectPath, String outputPath) throws IOException {
        AnalysisResult result = new ProjectAnalyzer(Path.of(projectPath)).analyze();
        AnalysisResultWriter.write(Path.of(outputPath), result);
    }
}
