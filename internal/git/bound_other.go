//go:build !unix

package git

import (
	"context"
	"io/fs"
	"os/exec"
)

// boundCheckout is the platform stub: this platform has no descriptor-bound
// execution, openBound always fails with errBoundUnsupported, and an agent-origin
// handle runs hardened and pinned but path-based, as in the first hardening delivery.
type boundCheckout struct{}

func openBound(string) (*boundCheckout, error)              { return nil, errBoundUnsupported }
func (b *boundCheckout) attach(_, _ string) error           { return errBoundUnsupported }
func (b *boundCheckout) Close()                             {}
func (b *boundCheckout) verify() error                      { return nil }
func (b *boundCheckout) run(cmd *exec.Cmd) error            { return cmd.Run() }
func (b *boundCheckout) identity() Identity                 { return Identity{} }
func (b *boundCheckout) dotGit() (DotGit, error)            { return DotGit{}, errBoundUnsupported }
func (b *boundCheckout) existsInGitDir(string) (bool, bool) { return false, false }
func (b *boundCheckout) boundPath() string                  { return "" }
func (b *boundCheckout) gitDirFile(string) ([]byte, error) {
	return nil, errBoundUnsupported
}
func (b *boundCheckout) command(context.Context, string, []string) (*exec.Cmd, []string, error) {
	return nil, nil, errBoundUnsupported
}
func (b *boundCheckout) readUnder(string) ([]byte, bool, error) {
	return nil, false, errBoundUnsupported
}
func (b *boundCheckout) writeUnder(string, []byte) error { return errBoundUnsupported }
func (b *boundCheckout) lstatUnder(string) (fs.FileMode, int64, error) {
	return 0, 0, errBoundUnsupported
}
