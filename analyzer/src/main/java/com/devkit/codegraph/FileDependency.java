package com.devkit.codegraph;

public class FileDependency {
    public String filePath;
    public String dependsOnFilePath;

    public FileDependency() {}

    public FileDependency(String filePath, String dependsOnFilePath) {
        this.filePath = filePath;
        this.dependsOnFilePath = dependsOnFilePath;
    }
}
