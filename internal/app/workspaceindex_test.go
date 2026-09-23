package app

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/codegraph"
)

type failingFor struct{ fail string }

func (a failingFor) Analyze(_ context.Context, path string) (codegraph.AnalysisResult, error) {
	if filepath.Clean(path) == a.fail {
		return codegraph.AnalysisResult{}, fmt.Errorf("%w: java not found", codegraph.ErrToolchainIncomplete)
	}
	return codegraph.AnalysisResult{Nodes: []codegraph.Node{{ID: "n", Label: "A.a", Kind: "method"}}}, nil
}

// Adding a workspace queues its services; a failure is written back to the
// registry and is what a later status read reports; an index on disk is not
// queued again at startup, and neither is a stored failure.
func TestWorkspaceServicesAreIndexedWhenAdded(t *testing.T) {
	c := NewContainer()
	c.ConfigDir = t.TempDir()
	c.CodeGraph.SetConfigDir(c.ConfigDir)
	good, bad := t.TempDir(), t.TempDir()
	c.CodeGraphRegistry.SetAnalyzer(failingFor{fail: bad})
	t.Cleanup(func() { _ = c.CodeGraphRegistry.Shutdown() })

	done := make(chan codegraph.ServiceIndexStatus, 16)
	WireWorkspaceIndex(c, func(st codegraph.ServiceIndexStatus) {
		if st.State != codegraph.IndexPending && st.State != codegraph.IndexIndexing {
			done <- st
		}
	})

	for _, root := range []string{good, bad} {
		if _, _, err := c.Workspace.AddWorkspace("", root); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]codegraph.ServiceIndexStatus{}
	for len(got) < 2 {
		select {
		case st := <-done:
			got[st.Path] = st
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out; got %+v", got)
		}
	}
	if got[good].State != codegraph.IndexIndexed {
		t.Fatalf("good = %+v", got[good])
	}
	if st := got[bad]; st.State != codegraph.IndexFailed || !st.MissingToolchain {
		t.Fatalf("bad = %+v", st)
	}

	all, err := c.Workspace.AllServices()
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range all {
		st := ServiceIndexStatus(c, svc)
		switch svc.Path {
		case good:
			if svc.IndexState != "" || st.State != codegraph.IndexIndexed {
				t.Fatalf("good stored %q, status %+v", svc.IndexState, st)
			}
		case bad:
			if svc.IndexState != "failed" || st.State != codegraph.IndexFailed || !st.MissingToolchain {
				t.Fatalf("bad stored %q, status %+v", svc.IndexState, st)
			}
		}
	}

	// Nothing left to queue: one is on disk, the other failed on record.
	QueueUnindexedServices(c)
	select {
	case st := <-done:
		t.Fatalf("startup queued %+v again", st)
	case <-time.After(200 * time.Millisecond):
	}
}
