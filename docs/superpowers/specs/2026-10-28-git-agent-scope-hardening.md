# Git tools: keep agent calls inside their repository, and off the host's execution

Pull request description. Steps 1 and 2 of design 3.5 in
`docs/reviews/2026-10-28-r1a-fc-o2-dirswap-review.md` (section 3, probes 3 and 4). Steps 3 and 4
(descriptor-bound execution, probes 1 and 2, the directory swap) are not in this change.

## Summary

Two weaknesses were reproduced against `8c5e64d0` with the review's probes:

1. `.git` indirection (probe 3). A granted folder whose `.git` is a file `gitdir: <other repo>/.git`
   passed the `Toplevel` check, and the handle then read and wrote the other repository.
2. Host execution (probe 4). A granted repository's own `core.fsmonitor` command ran in the host's git,
   outside the jail. An agent can write `.git/config`, hooks and `.gitattributes` in its own work tree.

### What changed

`internal/mcp/git_repo.go`, `gitFor` (agent callers only; identity from `internal/identity`):

- After the repository opens and the existing `Toplevel` check passes, `confineToScope` lstats
  `<top>/.git`.
  - A directory is accepted, as before, but it must be the git directory git itself found.
  - A file is parsed (`gitdir:`, relative paths relative to the file) and the target must equal the git
    directory git found and be inside the workspace folders or the granted roots, the scope the request
    was judged against.
  - A link must resolve to the git directory git found, inside the same scope.
  - The git directory's `commondir` (a file git follows, in a `.git` directory as well) must equal git's
    common directory and be inside the same scope.
  - A checkout with no `.git` at its top is refused. Refusal reuses the existing public code
    `git.repoOutsideWorkspace`; no new code, so nothing to add to `bridge.keptCodes`.
- A linked worktree is also accepted when it is the two halves of a real worktree: the git directory is
  `<common>/worktrees/<name>` and the `gitdir` file git wrote there names this very `.git` file. This is
  needed because a code-editing child is granted its checkout alone (an existing test,
  `TestGitCommitThroughMCPKeepsParentGuardInLinkedWorktree`, models it), so the repository it was cut from
  is not one of its roots.
- The handle is then pinned (`Service.Confine`): every later command gets `GIT_DIR` and `GIT_WORK_TREE`
  set to what was checked, so a later rewrite of the `.git` file, a `core.worktree`, or discovery from a
  sub folder cannot select another repository.

`internal/git/harden.go` and `git.go` (callers of kind agent only, decided from `identity.FromContext` when
the handle is opened by `Service.At`, and again for each call, so a plain context passed to an agent's
handle, as the review's probe does, stays hardened):

- Static neutralisation passed as `-c`: `core.fsmonitor=false`, `core.hooksPath=/dev/null`,
  `core.sshCommand=<GIT_SSH or ssh>`, `core.pager=cat`, `core.askPass=`, `core.gitProxy=`,
  `credential.helper=`, `diff.external=true`, `protocol.ext.allow=never`, `protocol.fd.allow=never`,
  signing off (`commit/tag/push.gpgSign=false`, `log.showSignature=false`, `merge.verifySignatures=false`),
  and the default gpg programs.
- Drivers the repository names are found per command (`git config --list -z`) and neutralised by name,
  because the agent picks the names: `filter.<n>.clean|smudge|process` empty and `required=false`,
  `diff.<n>.command=true`, `diff.<n>.textconv=cat`, `merge.<n>.driver=false` (an empty command would be a
  shell that succeeds and report a clean merge of nothing), `credential.<url>.helper` empty.
- Remote programs: `remote.<n>.uploadpack|receivepack` are not beaten by `-c` (git keeps the first value), so
  push, fetch and pull pass `--receive-pack=` and `--upload-pack=` explicitly. For a remote that is a local
  path or `file://`, the receiving side is started as `<git> -c core.hooksPath=/dev/null -c
  receive.denyCurrentBranch=refuse receive-pack`: the review did not list it, but the hooks of a local bare
  remote run in the host's process tree, and `-c` does not reach them (git clears its configuration
  environment for a local remote).
- Environment: `GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, `GCM_INTERACTIVE=never`,
  `GIT_PAGER=cat`, `GIT_EDITOR=true`, `GIT_SEQUENCE_EDITOR=true`; inherited `GIT_DIR`, `GIT_WORK_TREE`,
  `GIT_COMMON_DIR`, `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`,
  `GIT_NAMESPACE`, `GIT_EXTERNAL_DIFF`, `GIT_ASKPASS`, `SSH_ASKPASS`, `GIT_PAGER`, `GIT_EDITOR`,
  `GIT_SEQUENCE_EDITOR` are dropped first. What the host decides for a command (an account's askpass and
  secret, a key's `GIT_SSH_COMMAND`, the committer identity) is appended after, so it wins.
- `git.repos` (`Summaries`) and `Repositories` are hardened the same way.
- The person's terminal and every non-agent caller run exactly the old command line and environment
  (`TestTheHostsOwnCredentialEnvironmentSurvivesHardening` compares them).

`core.hooksPath` points at `/dev/null` (`NUL` on Windows), not at a temporary empty directory: a directory
under the temp roots may be writable by the agent (the jail allows session temp roots), and a hook planted
there would run in the host. A hook looked up as `/dev/null/<name>` does not exist.

## Tradeoffs the owner should know

- Agent calls no longer run the repository's own hooks (pre-commit, commit-msg, pre-push, ...), its LFS and
  other clean/smudge filters, its credential helpers, its signing program (agent commits are not signed
  even if `commit.gpgsign` is set, in the repository or in the person's global config), its
  `core.sshCommand`, or a configured merge driver or textconv. A repository that relies on a hook to
  enforce checks, or on LFS, behaves differently for agent commits: the checks do not run, and LFS-tracked
  files are committed as plain content. The person's own commits are unchanged.
- A bound HTTPS account still reaches git through `GIT_ASKPASS`, a bound SSH key through
  `GIT_SSH_COMMAND`. A remote with no bound account that relied on the person's credential helper (keychain)
  now fails for the agent with git's own authentication error; with no helper and
  `GIT_TERMINAL_PROMPT=0` that is the same as before.
- The push gates, the task and branch guards, task ownership and the protected-branch rule are untouched;
  there is still no force push.
- A linked worktree of a repository outside every workspace and grant is accepted only through its own
  worktree entry (see above); a submodule or worktree whose git directory is outside the scope and is not
  its own entry is refused, for example a linked worktree of a monorepo whose workspace folder is a
  sub folder of it.
- Cost: one extra `git config --list` process for each agent command that is not `rev-parse`.

## Evidence

On `8c5e64d0` (origin/main) with the review's probes 3 and 4, copied into `internal/mcp`:

```
--- FAIL: TestAgentGitDoesNotRunTheRepositorysFsmonitor
    HOST EXEC: the repository's core.fsmonitor command ran in the host's git process
--- FAIL: TestAgentGitRefusesAGitdirFileThatLeavesTheScope
    handle for .../pkg answers from: "VICTIM-REPO-OUTSIDE-THE-GRANT"
    GITDIR FILE: a granted folder redirected to a repository outside the grant, no race needed
```

With the change both pass: probe 4 creates no marker and probe 3 is refused with
`git.repoOutsideWorkspace`. Mutation checks: with `confineToScope` disabled the six refusal tests fail; with
`isHardened` forced false the eight hardening tests fail.

Probes 1 and 2 (directory swap) are left out on purpose; they need descriptor-bound execution, which is the
follow-up delivery.

## Test plan

Added: `internal/mcp/git_agent_scope_test.go` (the two probes), `git_agent_scope_checkout_test.go` (linked
worktree beside the repository and as a child's own checkout, worktree of a repository outside every scope,
submodule, `gitdir:` absolute, relative and through a dot, `commondir` outside in a worktree entry and in a
`.git` directory, a `.git` link out, ordinary repositories and sub folders, the person's terminal
unchanged, the pin after a `.git` rewrite), and `internal/git/harden_test.go` (hooks, filters, textconv,
external diff, merge drivers, credential helper, `remote.*.uploadpack|receivepack`, `core.hooksPath` and
the bare remote's own hooks, push over an ssh remote through a fake `ssh` with a hostile `core.sshCommand`,
host-supplied askpass and ssh environment surviving, driver discovery by name, summaries, the pin, and
worktree add/list/remove on a pinned handle). Push through a local bare remote is the existing path
(`TestPushSendsTheBranchAndSetsUpstream` and the gate tests still pass) and now also runs hardened.

```
go vet ./internal/mcp/ ./internal/git/
go test -p 1 -count=1 ./internal/mcp/          ok
go test -p 1 -count=1 ./internal/git/          one failure, also on origin/main (see limits)
go test ./internal/arch/                        ok (needs a placeholder frontend/dist)
gofmt -l internal/mcp internal/git              empty
```

## Limits

Not closed here:

- Directory swap (probes 1 and 2): a granted folder renamed away and replaced by a link, between the check
  and any later command, still redirects a path-based handle. The pin removes the `.git` discovery route
  but not a replaced directory. Descriptor-bound execution is a separate delivery.
- The neutralisation is a denylist of what git is known to execute, not a sandbox. The stronger option in
  the review (run agent-origin git under the session's own sandbox profile) remains an owner decision.
- The driver listing is read for each command; a driver added in the instant between the listing and the
  command is not covered.
- The `linked worktree` acceptance rests on the worktree entry naming the checkout back. A stale entry of a
  repository outside the scope that still names a path the agent can recreate would pass; it needs that
  repository to have had a worktree in a granted folder.
- `objects/info/alternates` and a repository's `url.<base>.insteadOf` are not examined. `git.repos` reads
  the state of workspace folders (hardened, but without the `.git` check), so a redirected `.git` file can
  reveal branch and change counts of another repository there. An agent with no working directory is given
  the app's open repository as before; its commands are hardened by the call's identity, not pinned.
- Windows and macOS were not exercised. The fixtures use `sh` hooks and a fake `ssh` script, so those tests
  skip on Windows; on macOS only the Linux run was done, and the `/dev/null` hooks path and the path
  resolution of `/var` against `/private/var` (handled with `EvalSymlinks` throughout) were not run there.
- `TestResolveSideTakesTheStageExactly` in `internal/git` fails on origin/main in this environment too
  (line endings from the machine's own git configuration), and `internal/arch` needs `frontend/dist` to
  exist. Neither is caused by this change.
