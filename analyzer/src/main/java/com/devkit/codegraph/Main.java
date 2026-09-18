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
        } catch (OutOfMemoryError e) {
            // Errors are not Exceptions, so without this an out-of-memory run
            // died with a bare JVM stack trace the caller had to decode. The
            // heap is capped by the launcher (-Xmx), so say that plainly.
            System.err.println("Error analyzing project: ran out of memory (max heap "
                    + Runtime.getRuntime().maxMemory() / (1024 * 1024) + " MB). "
                    + "The project is too large to index within this limit; raise -Xmx to try again.");
            System.exit(3);
        } catch (StackOverflowError e) {
            System.err.println("Error analyzing project: stack overflow while analyzing "
                    + "(a deeply nested expression or type hierarchy); raise -Xss to try again.");
            System.exit(4);
        }
    }

    public static void run(String projectPath, String outputPath) throws IOException {
        AnalysisResult result = new ProjectAnalyzer(Path.of(projectPath)).analyze();
        AnalysisResultWriter.write(Path.of(outputPath), result);
    }
}
