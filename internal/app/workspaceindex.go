package app

import (
	"log"

	"devkit/internal/codegraph"
	"devkit/internal/workspace"
)

// WireWorkspaceIndex joins the workspace registry to Code Graph's index
// queue (docs/dev-kit/workspace.mdx § "Thêm service" and § "Trạng thái
// index"): a service registered is queued for its one index, and the queue's
// outcomes that must outlive a restart — failed, unsupported — are written
// back to workspace_services. The two packages do not import each other;
// this is where they meet. onStatus sees every transition, for the UI.
func WireWorkspaceIndex(c *Container, onStatus func(codegraph.ServiceIndexStatus)) {
	reg := c.CodeGraphRegistry
	if reg == nil || c.Workspace == nil {
		return
	}
	c.Workspace.OnServicesAdded = func(services []workspace.WorkspaceService) {
		paths := make([]string, 0, len(services))
		for _, svc := range services {
			paths = append(paths, svc.Path)
		}
		reg.Enqueue(paths...)
	}
	reg.OnIndexStatus = func(st codegraph.ServiceIndexStatus) {
		switch st.State {
		case codegraph.IndexIndexed, codegraph.IndexFailed, codegraph.IndexUnsupported:
			if err := c.Workspace.RecordIndexOutcome(st.Path, string(st.State), st.Error, st.MissingToolchain); err != nil {
				log.Printf("workspace: record index outcome for %s: %v", st.Path, err)
			}
		}
		if onStatus != nil {
			onStatus(st)
		}
	}
}

// QueueUnindexedServices queues every registered service that has no index
// on disk and no stored failure — what a restart mid-queue left behind, since
// pending and indexing are never persisted. A failure is not retried on its
// own: it stays until someone asks (WorkspaceReindexService) or a rescan.
func QueueUnindexedServices(c *Container) {
	reg := c.CodeGraphRegistry
	if reg == nil || c.Workspace == nil {
		return
	}
	services, err := c.Workspace.AllServices()
	if err != nil {
		log.Printf("workspace: list services to index: %v", err)
		return
	}
	var paths []string
	for _, svc := range services {
		if svc.IndexState != "" {
			continue
		}
		if _, ok := codegraph.StoredIndex(c.ConfigDir, svc.Path); ok {
			continue
		}
		paths = append(paths, svc.Path)
	}
	if len(paths) > 0 {
		reg.Enqueue(paths...)
	}
}

// ServiceIndexStatus is one service's index state for the UI: the Registry's
// (queued, running, on disk) over the stored outcome, which is what answers
// when nothing has run this session.
func ServiceIndexStatus(c *Container, svc workspace.WorkspaceService) codegraph.ServiceIndexStatus {
	var st codegraph.ServiceIndexStatus
	if c.CodeGraphRegistry != nil {
		st = c.CodeGraphRegistry.IndexStatus(svc.Path)
	} else {
		st = codegraph.ServiceIndexStatus{Path: svc.Path}
	}
	if st.State == codegraph.IndexUnknown && svc.IndexState != "" {
		st.State = codegraph.IndexState(svc.IndexState)
		st.Error = svc.IndexError
		st.MissingToolchain = svc.IndexMissingToolchain
	}
	return st
}
