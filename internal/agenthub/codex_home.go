package agenthub

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// codexPolicyEntries are the files and directories in CODEX_HOME that steer
// Codex itself rather than record what it did. The jail may not write them
// (sandbox.Policy.DenyWritePaths), and DevKit resets them before every spawn:
//
//	config.toml         a writable one lets the agent trust a repository for
//	                    every later session (Phase 0: that is how a project's
//	                    .codex/config.toml gets loaded).
//	rules/              exec policy; an "allow" rule approves commands without
//	                    asking, possibly inside the session that wrote it.
//	AGENTS.md,          global instructions: a prompt injected into every
//	AGENTS.override.md  later session.
//	skills/             global skills, for the same reason.
//
// Read-only is not enough on its own. The jail cannot write these, but a
// session from before this rule, a symlink planted where one of them should
// be, or anything else that got there is still read by the next spawn — so
// DevKit empties them each time, and what Codex reads is what DevKit wrote.
//
// Measured on 0.147.0: Codex starts and runs turns with all five read-only.
// It logs "failed to install system skills" once per start, because it
// copies its bundled skills into skills/.system on every start and cannot;
// those skills (imagegen, openai-docs, skill-creator, …) are simply absent.
var codexPolicyEntries = []struct {
	name string
	dir  bool
}{
	{"config.toml", false},
	{"rules", true},
	{"AGENTS.md", false},
	{"AGENTS.override.md", false},
	{"skills", true},
}

// codexConfigHeader is the whole of the config.toml DevKit writes. Everything
// DevKit sets goes on the command line or in each request, where the agent
// cannot reach it; the file exists only so the jail has something to bind
// read-only.
const codexConfigHeader = "# Managed by DevKit and rewritten before every Codex session.\n" +
	"# Settings here are ignored: DevKit passes its own on the command line.\n"

// prepareCodexHome makes home ready for a spawn and returns the paths the
// jail must not write. The home is created 0700 and must be a real
// directory: a symlink there would move the whole grant somewhere else.
func prepareCodexHome(home string) ([]string, error) {
	if home == "" || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("%w: codex home must be an absolute path", ErrProcessFailed)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("%w: codex home: %v", ErrProcessFailed, err)
	}
	info, err := os.Lstat(home)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: codex home %s is not a directory", ErrProcessFailed, home)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return nil, fmt.Errorf("%w: codex home: %v", ErrProcessFailed, err)
	}
	deny := make([]string, 0, len(codexPolicyEntries))
	for _, entry := range codexPolicyEntries {
		path := filepath.Join(home, entry.name)
		if err := resetPolicyEntry(path, entry.dir); err != nil {
			return nil, fmt.Errorf("%w: codex home: reset %s: %v", ErrProcessFailed, entry.name, err)
		}
		deny = append(deny, path)
	}
	return deny, nil
}

// resetPolicyEntry leaves path an empty directory or a file holding only
// what DevKit writes. Anything of the wrong kind — a symlink above all — is
// removed first rather than followed. A directory is emptied in place, not
// replaced: a session already running binds it, and on Linux replacing it
// would leave that session looking at the old one.
func resetPolicyEntry(path string, dir bool) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case dir && info.IsDir(), !dir && info.Mode().IsRegular():
	default:
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	if dir {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(path, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	content := ""
	if filepath.Base(path) == "config.toml" {
		content = codexConfigHeader
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
