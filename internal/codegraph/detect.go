// internal/codegraph/detect.go
package codegraph

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrUnsupportedProject is returned when a folder holds nothing any registered
// toolchain recognises.
//
// Worth its own error because the alternative was worse: without a check,
// OpenProject launched the analyzer at any folder at all, waited for a JVM to
// start and walk it, and got back an empty index — reported as an empty
// project rather than as the wrong folder. Refusing before the subprocess
// starts is both faster and the only version that can say why.
var ErrUnsupportedProject = errors.New("codegraph: no supported project found")

// DetectedProject is what a folder turned out to hold.
type DetectedProject struct {
	Language string `json:"language"`
	Label    string `json:"label"`
	// Modules are the module roots found, relative to the folder that was
	// opened, "." meaning the folder itself. A repository of microservices has
	// one entry per service; a single project has exactly one.
	Modules []string `json:"modules"`
}

// detectLimits bound the walk. A project folder can be a monorepo with tens of
// thousands of directories, and detection runs before the user has been shown
// anything, so it must be fast and must not wander into dependency trees.
const (
	// maxDetectDepth is how far below the opened folder a module may sit.
	// Services in a monorepo are conventionally one or two levels down
	// (services/orders, apps/backend/orders); four leaves room without
	// turning the walk into a full crawl.
	maxDetectDepth = 4
	// maxDetectDirs caps the total directories visited, so a pathological tree
	// cannot stall the open.
	maxDetectDirs = 20000
)

// skipDirs are never descended into: dependency caches, build output and VCS
// metadata. They hold no source the user wrote, they are where the file count
// explodes, and a build directory full of generated sources would otherwise
// register as a module of its own.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "venv": true, ".venv": true,
	"target": true, "build": true, "out": true, "dist": true, "bin": true,
	".gradle": true, ".idea": true, ".vscode": true, ".devkit": true,
}

// Detect reports whether root holds a project this toolchain can analyze, and
// where its modules are.
//
// A module is a directory holding one of the toolchain's Markers — a build
// file, in practice. That is what makes a folder of microservices work: each
// service has its own build file, so each registers as its own module and the
// caller learns there are several rather than assuming one.
//
// The fallback matters as much as the markers. A folder with source files and
// no build file at all is still a project someone wants to read, so when no
// marker is found anywhere but the language's source extension is, the opened
// folder itself is the single module.
func (t Toolchain) Detect(root string) (DetectedProject, bool) {
	if len(t.Markers) == 0 && t.SourceExt == "" {
		return DetectedProject{}, false
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return DetectedProject{}, false
	}

	modules := map[string]bool{}
	sawSource := false
	visited := 0

	// WalkDir rather than Glob per candidate: one pass, and the skip list can
	// prune whole subtrees, which is what keeps a monorepo affordable.
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory is not a reason to fail the detection —
			// skip it and judge on what is readable.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if visited > maxDetectDirs {
			return filepath.SkipAll
		}

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		depth := 0
		if relative != "." {
			depth = strings.Count(relative, string(filepath.Separator)) + 1
		}

		if entry.IsDir() {
			visited++
			if path == root {
				return nil
			}
			name := entry.Name()
			if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
				return fs.SkipDir
			}
			if depth >= maxDetectDepth {
				return fs.SkipDir
			}
			return nil
		}

		if t.SourceExt != "" && strings.HasSuffix(entry.Name(), t.SourceExt) {
			sawSource = true
		}
		for _, marker := range t.Markers {
			if matched, _ := filepath.Match(marker, entry.Name()); matched {
				dir := filepath.Dir(relative)
				if dir == "" {
					dir = "."
				}
				modules[dir] = true
				break
			}
		}
		return nil
	})

	if len(modules) == 0 {
		if !sawSource {
			return DetectedProject{}, false
		}
		// Source with no build file: the folder itself is the project.
		modules["."] = true
	}

	found := make([]string, 0, len(modules))
	for dir := range modules {
		found = append(found, dir)
	}
	sort.Strings(found)
	return DetectedProject{Language: t.Language, Label: t.Label, Modules: found}, true
}

// DetectProject reports what registered toolchain, if any, recognises a folder.
//
// First match wins rather than every match: one folder is analyzed by one
// analyzer, and a repository holding both a Java service and a TypeScript one
// is a real thing the module will have to answer for properly when a second
// analyzer exists. Reporting the first is honest today and is the seam where
// that answer will go.
func (s *Service) DetectProject(root string) (DetectedProject, error) {
	for _, toolchain := range s.toolchainList() {
		if detected, ok := toolchain.Detect(root); ok {
			return detected, nil
		}
	}
	return DetectedProject{}, ErrUnsupportedProject
}
