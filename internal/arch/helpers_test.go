package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot is the module root (internal/arch is two levels below it).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// prodFile is one parsed, non-test Go file of the module's own code. Only
// internal/, cmd/ and the root package are scanned: never frontend/, build/ or the
// checkouts agents keep under .claude/.
type prodFile struct {
	Rel  string // slash-separated path from the module root
	Dir  string // slash-separated directory from the module root
	Src  []byte
	File *ast.File
	Fset *token.FileSet
}

func prodFiles(t *testing.T) []prodFile {
	t.Helper()
	root := repoRoot(t)
	var out []prodFile
	add := func(path string) {
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		out = append(out, prodFile{Rel: rel, Dir: filepath.ToSlash(filepath.Dir(rel)), Src: src, File: f, Fset: fset})
	}
	for _, top := range []string{"internal", "cmd"} {
		_ = filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			add(path)
			return nil
		})
	}
	rootGo, _ := filepath.Glob(filepath.Join(root, "*.go"))
	for _, path := range rootGo {
		if !strings.HasSuffix(path, "_test.go") {
			add(path)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

// lines counts the lines of a file.
func (f prodFile) lines() int { return strings.Count(string(f.Src), "\n") + 1 }

// selector reports whether call is pkg.name(...), by the identifier the file
// imported the package under (the standard library names are never aliased in
// this module; an alias would only make a rule miss, not misfire).
func selector(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}
