package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"devkit/internal/identity"
)

// An agent can write inside its own work tree, and that includes .git/config,
// .git/hooks and .gitattributes. Git runs programs those files name: a
// core.fsmonitor command, a hook, a clean/smudge filter, a merge driver, a
// credential helper, ssh. This service runs in the host process tree, outside
// the agent's jail, so for a call that comes from an agent (the caller identity
// the server already carries) the repository-supplied execution is turned off
// instead of honoured. The person's own calls keep the repository's own
// configuration, as the package comment says.
//
// What this costs an agent call: the repository's hooks, LFS and other
// filters, its credential helpers and its signing program do not run. The
// push gates, the task and branch guards and the account bound to a remote are
// not touched; a bound HTTPS account still reaches git through GIT_ASKPASS and
// a bound SSH key through GIT_SSH_COMMAND, both added after this environment.
//
// This is a denylist of what Git is known to execute. It is not a sandbox: the
// stronger option is to run agent-origin git under the session's own sandbox
// profile, which is an owner decision (see the design note that came with it).

// agentOrigin reports whether ctx carries an Agent Hub caller.
func agentOrigin(ctx context.Context) bool {
	caller, ok := identity.FromContext(ctx)
	return ok && caller.IsAgent()
}

// noHooksPath is where core.hooksPath points for an agent call. It is not a
// directory the agent could write into: a hook looked up as /dev/null/<name>
// does not exist. (A temporary "empty directory" would be a path the agent may
// be able to write into, and a hook planted there would run in the host.)
func noHooksPath() string {
	if runtime.GOOS == "windows" {
		return "NUL"
	}
	return "/dev/null"
}

// neutralConfig is applied to every agent-origin command. Each entry replaces
// what the repository, or the person's own configuration, says for that key.
func neutralConfig() []string {
	// core.sshCommand beats GIT_SSH and loses to GIT_SSH_COMMAND. An empty value
	// is not "unset": git then tries to run "". The neutral value is what git
	// would run anyway.
	ssh := "ssh"
	if v := strings.TrimSpace(os.Getenv("GIT_SSH")); v != "" {
		ssh = v
	}
	return []string{
		"core.fsmonitor=false",
		"core.hooksPath=" + noHooksPath(),
		"core.sshCommand=" + ssh,
		"core.pager=cat",
		"core.askPass=",
		"core.gitProxy=",
		"credential.helper=",
		"diff.external=true",
		"protocol.ext.allow=never",
		"protocol.fd.allow=never",
		"commit.gpgSign=false",
		"tag.gpgSign=false",
		"push.gpgSign=false",
		"log.showSignature=false",
		"merge.verifySignatures=false",
		"gpg.program=gpg",
		"gpg.openpgp.program=gpg",
		"gpg.x509.program=gpgsm",
		"gpg.ssh.program=ssh-keygen",
	}
}

// neutralValue is what a named driver's program is replaced by. Never "": a
// shell given an empty command succeeds, which would make an empty merge driver
// report a clean merge of nothing, and git cannot run an empty diff command.
func neutralValue(section, variable string) (string, bool) {
	switch section + "." + variable {
	case "filter.clean", "filter.smudge", "filter.process":
		return "", true // git skips a filter with no command
	case "filter.required":
		return "false", true
	case "diff.command":
		return "true", true
	case "diff.textconv":
		return "cat", true
	case "merge.driver":
		return "false", true // fails: the merge is a conflict, never a wrong clean result
	case "credential.helper":
		return "", true
	}
	return "", false
}

// repositoryDrivers reads the names the repository (and the configuration above
// it) gave to filters, diff and merge drivers and credential helpers, and returns
// the -c arguments that neutralise each. They cannot be listed in advance because
// the agent picks the names. The listing is read for each command; a driver
// added between that read and the command is a window this does not close.
func (r runner) repositoryDrivers(ctx context.Context) []string {
	cmd := exec.CommandContext(ctx, r.git, "--no-optional-locks", "-C", r.work, "config", "--list", "-z")
	cmd.Env = r.hardenedEnv(cmd.Environ(), nil)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var args []string
	seen := map[string]bool{}
	for _, entry := range bytes.Split(out, []byte{0}) {
		key, _, _ := strings.Cut(string(entry), "\n")
		first, last := strings.Index(key, "."), strings.LastIndex(key, ".")
		if first < 0 || first == last {
			continue // no subsection: the static list owns it
		}
		section, variable := strings.ToLower(key[:first]), strings.ToLower(key[last+1:])
		value, ok := neutralValue(section, variable)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		args = append(args, "-c", key+"="+value)
	}
	return args
}

// ownEnv is what an agent-origin command must not inherit: it would pick the
// repository, or run a program, in place of the one this service chose.
var ownEnv = map[string]bool{
	"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_COMMON_DIR": true, "GIT_INDEX_FILE": true,
	"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_NAMESPACE": true,
	"GIT_EXTERNAL_DIFF": true, "GIT_ASKPASS": true, "SSH_ASKPASS": true,
	"GIT_PAGER": true, "GIT_EDITOR": true, "GIT_SEQUENCE_EDITOR": true,
}

// hardenedEnv is base without ownEnv, plus the fixed settings of an agent call
// and, when the repository was pinned, GIT_DIR and GIT_WORK_TREE. extra is added
// last: it is what the host itself decided (an account's askpass, a key's ssh
// command, the committer identity).
func (r runner) hardenedEnv(base, extra []string) []string {
	env := make([]string, 0, len(base)+16+len(extra))
	for _, kv := range base {
		if name, _, _ := strings.Cut(kv, "="); !ownEnv[name] {
			env = append(env, kv)
		}
	}
	env = append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GIT_PAGER=cat",
		"GIT_EDITOR=true",
		"GIT_SEQUENCE_EDITOR=true",
	)
	if r.gitDir != "" {
		env = append(env, "GIT_DIR="+r.gitDir)
		if r.workTree != "" {
			env = append(env, "GIT_WORK_TREE="+r.workTree)
		}
	}
	return append(env, extra...)
}

// subcommand is the first argument that is not an option.
func subcommand(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// remoteFlags are the options that name the program git runs at the other end
// of a local or ssh remote. remote.<name>.uploadpack and .receivepack would run
// whatever the repository says, and a -c override does not beat them (git keeps
// the first value it reads), but an option does.
func (r runner) remoteFlags(ctx context.Context, command string) []string {
	if !r.isHardened(ctx) {
		return nil
	}
	switch command {
	case "push":
		return []string{"--receive-pack=git-receive-pack"}
	case "fetch", "pull":
		return []string{"--upload-pack=git-upload-pack"}
	}
	return nil
}

func (r runner) isHardened(ctx context.Context) bool { return r.hardened || agentOrigin(ctx) }

// sibling is a runner for another work tree of the same service: it keeps the
// hardening and drops the pin, which belongs to one repository.
func (r runner) sibling(path string) runner {
	return runner{git: r.git, work: path, hardened: r.hardened}
}
