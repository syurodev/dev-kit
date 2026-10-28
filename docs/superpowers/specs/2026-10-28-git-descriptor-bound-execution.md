# Git tools: descriptor-bound execution for agent calls (directory swap, stage 2)

Pull request description. Steps 3 and 4 of design 3.5 in
`docs/reviews/2026-10-28-r1a-fc-o2-dirswap-review.md` (probes 1 and 2, the directory swap). Stage 1
(`docs/superpowers/specs/2026-10-28-git-agent-scope-hardening.md`, PR #386) closed the `.git` file redirect
(probe 3) and repository-supplied execution (probe 4).

This change narrows the directory swap a great deal. It does **not** make the Git scope a boundary against a
hostile agent, and it does not fully close the swap for commands that read the work tree (see "Residual risks").

## The problem

After `gitFor` opened a repository, the handle ran every command as `git -C <path>`, so the kernel looked the
path up again each time. An agent that can write in its granted folder can rename the directory away and put a
link to another repository in its place; the same handle then answers from the other repository. Reproduced on
`origin/main` (`b33f4391`) with the review's probes, inverted into tests:

```
--- FAIL: TestAgentGitHandleSurvivesTheDirectoryBeingSwappedForALink
    DIRECTORY SWAP AFTER OPEN: ... answered [{... VICTIM-REPO-OUTSIDE-THE-GRANT ...}] after the swap
    DIRECTORY SWAP AFTER OPEN: status read the other repository's work tree: [VICTIM_ONLY.txt]
--- FAIL: TestAgentGitDirectorySwapRaceNeverAnswersFromAnotherRepository
    log: flips=146571 granted=335 refused=1477 victim=58 other=130        (2000 calls)
--- FAIL: TestAgentGitDirectorySwapRaceNeverReadsAnotherWorkTree
    status: flips=63024 granted=123 refused=792 victim=31 other=54        (1000 calls)
```

## What was built

### Descriptor-bound handle (`internal/git/bound*.go`, `internal/fchdirexec`)

For a handle opened by an agent-origin call (the caller identity the server already carries; the person's
terminal and every non-agent caller run exactly the old command line):

1. **Open without a path lookup.** `Service.At` resolves the folder once (`EvalSymlinks`), then walks it from `/`
   one component at a time with `openat(O_DIRECTORY|O_NOFOLLOW)`. A component that is, or has become, a link
   fails the walk and the handle is refused (`git.repoOutsideWorkspace`). `openat2` was not used: for a path
   that is already resolved the manual walk is the same thing, and it is the only option on macOS, so one walk
   serves both and is tested on Linux.
2. **Hold what git found.** After the first `rev-parse` (one process: toplevel, git directory, common directory)
   the handle holds the top of the checkout (reached by walking *up* through the held directory, then compared
   with what its name leads to) and the git directory (walked the same way). Device and inode of the folder, the
   top and the git directory are recorded (`Service.Identity`).
3. **Run git in the held directory, with no path lookup.**
   - Linux: `cmd.Dir = /proc/<host pid>/fd/<n>` (the kernel resolves the descriptor, not a name; `os/exec`
     changes into `Dir` in the child) and the git directory is inherited as a descriptor and given as
     `GIT_DIR=/proc/self/fd/<n>`; `GIT_WORK_TREE` is relative to the held directory (`.`, `..`, ...).
   - Other Unix: the app's own binary is started under `argv[0] = devkit-git-fchdir` with the directory as fd 3
     (`internal/fchdirexec`, an `init()` hook in a package that has only the standard library, so it runs before
     the heavy packages initialise): it does `fchdir(3)`, closes it and `execve`s git, so the process the caller
     waits for and cancels is git itself. No flag, subcommand or `main.go` change; the mode is chosen by
     `argv[0]`. The git directory is then named relative to the held directory (`.git`, `../.git`), or by
     absolute path for a linked worktree or submodule (its git directory is elsewhere).
4. **Identity check before and after every command** (`verify`): the folder, the top and the git directory must
   still be what their names lead to, else the command is refused or its answer discarded
   (`git.repoOutsideWorkspace`; the code already published in `bridge.keptCodes`, no new code).
5. **Linux watcher.** While a command runs, its working directory is read from `/proc/<pid>/cwd`; a process
   that is neither in the held directory nor in the directories between it and the top is killed and the
   command refused. This exists because of what the measurements below showed git does.
6. **Everything else that touched the work tree by path now goes through the held directory**: the conflict
   file read and write (`readWork`/`writeWork`, links refused), the untracked-patch size check, the
   "operation in progress" probe (asks the held git directory; `rev-parse --git-path` would print a descriptor
   path that means something else in the host). `confineToScope` reads `.git`, `commondir` and `gitdir`
   through the held descriptors (`Service.DotGit`, `GitDirFile`) and `gitFor` requires `BoundPath()` to be the
   resolved path the request was judged for.
7. Descriptors are closed with `Service.Close` (`closeGit` after each tool call; a handle that is dropped is
   closed when collected).

### Step 4: where guards read identity

The branch/repository guards and the push gates are looked up with the handle's policy key, a path string read
**from the held directory when the handle opened**, and the command that follows runs in that same held
directory. So the guard that is consulted is the one of the repository the command runs in, and a folder
swapped for another repository can no longer move the command there (`TestBoundGuardIsTheOneOfTheRepositoryTheCommandRunsIn`).
The key itself is still a path string, deliberately: guards and remote bindings are synced vault rows keyed by
repository path, and `(dev, ino)` is machine-local, so re-keying them would be a data migration. `Service.Identity`
exposes the verified `(dev, ino)` and top for callers that want to compare. Not changed: the PR tools resolve a
folder name again in `internal/prwatch`; the managed-writer resource key
(`internal/app/managed_ownership.go`) hashes `(root, gitDir)` strings for host-internal calls.

## What the measurements showed (and the design that followed)

The review's design said to run git "with `GIT_DIR`/`GIT_WORK_TREE` set to `.`-relative paths". Measured with
`strace`: **git re-resolves the work tree by NAME inside a single command.** It computes the name from
`getcwd`, and a command that needs the work tree (`status`, `diff`, `add`, `commit`, `checkout`) then calls
`chdir(<that absolute name>)`. A name that is rebound in between puts that one command into another directory.
`GIT_WORK_TREE=/proc/self/fd/N` does not help (git `realpath`s it back to a name). `GIT_DIR=/proc/self/fd/N`
is kept as given by git, so the **git directory can be bound completely on Linux**; the work tree cannot, with
stock git.

Ablation with the same flipper (counts per run of 1000 status calls, the repository's own answer and the work
tree's file list told apart):

| Variant | log, 2000 calls | status: other repository | status: other work tree |
|---|---|---|---|
| origin/main (path-based) | 58 (also 12) | 31 (files) | |
| A: bound handle removed | 21 and 7 | 37 and 19 (repository or files, not told apart) | |
| B: bound, no identity check, no watcher | 0 | 0 | 66, 22, 74, 33 |
| B + git directory by relative name | 0 | 40, 10, 34, 11 | 24, 5, 17, 6 |
| shipped | 0 | 0 | 0 to 2 |

So: the descriptors keep every answer that comes from the repository (refs, objects, configuration, index)
on the granted repository; the identity check and the watcher reduce the work-tree window from several percent
to a few in ten thousand, they do not remove it.

## Evidence

All on Linux/amd64, git 2.53.0, Go 1.27.1, no jail. `-p 1`, memory capped as in the task.

```
cd <worktree>; go test -p 1 -count=1 ./internal/mcp/ -run 'TestAgentGit(HandleSurvives|FreshOpen|DirectorySwap)' -v
TestAgentGitHandleSurvivesTheDirectoryBeingSwappedForALink          PASS (was FAIL)
TestAgentGitFreshOpenAfterTheSwapIsTheLinksTargetAndIsRefused       PASS
TestAgentGitDirectorySwapRaceNeverAnswersFromAnotherRepository      PASS  wall:   log: flips=67392 granted=216 refused=1784 victim=0
                                                                          bursts: log: flips=15140 granted=1458 refused=542 victim=0
TestAgentGitDirectorySwapRaceNeverReadsAnotherRepositoryAndBoundsTheWorkTree
                                                                    PASS  wall:   status: victim repository=0 victim work tree=0
                                                                          bursts: status: victim repository=0 victim work tree=0
```

Repeated 10 times (`-count=10`, about 30 s): 40,000 log calls and 20,000 status calls, **0 answers from the
other repository**; the other directory's files appeared in **13 of the 20,000 status calls** (0.065 per cent of
the calls, about 0.2 per cent of the 5,900 that opened the granted directory). The run is under 4 s with 2000
and 1000 calls per rhythm, a hard iteration cap, a flip cap of 400,000 and a deadline check. Under `-race` on
just that test (`-race ... -run 'TestAgentGitDirectorySwap|TestAgentGitHandleSurvives'`): no data race, same
counts. (`fakeAgentHub` in the test deps is not safe for concurrent use, so each worker has its own server.)

**Mutation check (recorded, then restored).** With the binding removed (`At` no longer opens a bound handle)
the same tests fail: probe 1 answers `VICTIM-REPO-OUTSIDE-THE-GRANT` and `VICTIM_ONLY.txt`; the log stress
shows 21 and 7 foreign answers, the status stress 37 and 19 (table above). With the identity check and watcher
removed the status test fails with 66, 22, 74 and 33 work-tree reads in 1000 calls (limit 10); with the git
directory also named by relative name, 40, 10, 34 and 11 status calls are answered by the other repository.

Unit tests: `internal/fchdirexec` (fchdir into the descriptor and not the name, environment and the next
descriptor passed through, fd 3 closed, exit codes 7/0/125/127, refusal with no descriptor and with a file,
cancellation kills the program, 8 MiB out and in, no program), `internal/git/bound_test.go` (swap refused;
descriptors alone answer correctly with the check made blind; linked worktree and submodule; a sub folder sees
what an unbound handle sees; push to a local bare remote through the guarded path with the remote's hook not
run; hardened environment and the host's own variables on the bound command; file reads/writes through the
held directory refuse links and escapes; in-progress probe; close; the walk; the guard), each of the mode-
dependent ones **twice: native and with every command forced through the helper**.

```
go vet ./internal/git/ ./internal/mcp/ ./internal/fchdirexec/                 clean
go test -p 1 -count=1 ./internal/git/                                         one failure, TestResolveSideTakesTheStageExactly (CRLF config of this machine, also on main)
go test -p 1 -count=1 ./internal/mcp/                                         ok
go test -count=1 -race ./internal/fchdirexec/                                 ok
go test ./internal/arch/                                                      ok (placeholder frontend/dist removed afterwards)
go test ./internal/bridge/ -run 'TestEveryMCPErrorCode|TestEveryGitSentinel'  ok
gofmt -l internal                                                             empty
GOOS=darwin|windows|freebsd go vet / go build (CGO off)                       compile only
```

## Tradeoffs

- **Linux cost** is small (no helper process): 12 ms for an open and one read, measured while the open still ran
  four `rev-parse` processes (it runs one now).
- **Elsewhere** each command starts through the helper, a second start of the app binary: measured 24 ms per
  command on Linux with the real app binary (a Wails build, 71 MB) and with the test binary alike. The open
  was cut from four `rev-parse` processes to one for a bound handle (macOS then pays about three helper starts
  for a read, not seven). **The macOS number was not measured.**
- Agent calls now refuse a folder that was renamed or replaced while a handle was open (`git.repoOutsideWorkspace`)
  also when the person renamed it; handles are per tool call, so that is a rare, short-lived case.
- `git.repos` / `Repositories` (`Summaries`) for an agent and a linked worktree created by `git.worktree.add`
  are hardened but path-based (the worktree is created at a path under the repository's top).
- The helper hook adds one `init()` that compares `argv[0]` in every binary that imports `internal/git`.
- New `Service` methods: `Close`, `Bound`, `BoundPath`, `Identity`, `DotGit`, `GitDirFile`.

## Residual risks (keep these in any description of the Git scope)

1. **Work tree, same command (Linux and macOS).** A command that needs the work tree (status, diff, add,
   commit, checkout, reset, merge) can be sent by git's own `chdir(name)` into another directory, for a few
   microseconds of window. Measured above: 13 of 20,000 status calls under a continuous `RENAME_EXCHANGE`
   flipper read the other directory's files. Effect for a read: the file names and content of a directory the
   agent could name (a link target) reach the agent through `git.status`/`git.diff`; for `add`/`commit`/
   `checkout`, files of that directory can be recorded in, or written from, the granted repository. The
   repository's own history, refs, configuration and objects are not reachable this way on Linux; on other Unix
   the git directory is named relative to the held directory, so the same window can reach the other directory's
   `.git` too. No `/proc` means no watcher on macOS, so only the before and after identity check applies there.
2. **A rebound and restored name** during a command is invisible to the identity check; the watcher sees a
   process only while it is in the other directory when it looks (every 20 microseconds or so).
3. **Paths git is given by name** are still resolved by the kernel when used: the target of `git.worktree.add`,
   remotes that are local paths, `submodule` and `core.worktree` targets. A linked worktree's or submodule's
   git directory is held and named by descriptor on Linux and by absolute path elsewhere.
4. **PR tools and managed-writer ownership** resolve a folder name again (`internal/prwatch`,
   `internal/app/managed_ownership.go`); guard and remote-binding keys are path strings in synced vault rows.
5. **Not a sandbox.** Repository-supplied execution is still a denylist (stage 1). The stronger options are
   running agent-origin git under the session's own sandbox profile, or Landlock rules built from the held
   descriptors on Linux (inode-based, and the only way found to bind the work tree against git's own
   `chdir`); both are owner decisions and were not attempted.
6. **A hostile agent that can also run a process outside the jail** or write in a directory above the granted
   folder is outside this model.

## What was not verified

- macOS: the helper path and the manual walk compile (`GOOS=darwin go vet`) and run on Linux (`forceHelper`,
  and the descriptor-only tests), but nothing ran on macOS: the `/dev/fd` behaviour, `fchdir` on the app
  binary under dyld/code signing, the cost of starting the app binary, `st_dev` types. The flipper was not run
  against the helper path (each helper start costs about 25 ms; only the swap-after-open and descriptor tests
  cover it).
- A real jail (bubblewrap, Seatbelt): none was used; neither was the agent's own view of `/proc`.
- Windows: stubs only (path-based, as in stage 1), builds.
- Kernel/runtime variance: `/proc/<pid>/fd` access by a child needs the host process to be dumpable (no code
  here clears it); if `chdir` into it were refused the command fails closed.
