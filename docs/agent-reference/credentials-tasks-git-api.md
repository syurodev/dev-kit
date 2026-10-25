### Credentials, work and the repository (`accounts`, `tasks`, `git`, `apiclient`)
Four modules added together; the design lives in `docs/dev-kit/{accounts,tasks,git,api-client}.mdx`
and the cross-module rules in `docs/dev-kit/cross-module.mdx`.

```bash
go test ./internal/accounts/ ./internal/tasks/ ./internal/git/ ./internal/apiclient/ ./internal/gitparse/ ./internal/notify/ -count=1
```

- **`internal/accounts` keeps two stores behind one service.** A *shared*
  account is a vault item and syncs; a *scoped* one (a single connection's
  password) goes to the local `account_secrets` table and does not. That is not
  a policy choice — `db_query_history` already wrote down why: everything in
  `vault_items` is sync-bound by construction, so a credential that must stay
  on one machine cannot live there. It is still sealed with the DEK. The link
  from a profile is `scope_owner`, deliberately **not** a stored `account_id`:
  an id pointing at a row that does not sync dangles on the next device, and
  writing a replacement back makes two devices overwrite each other forever.
- **No method anywhere returns a credential to an agent.** `AccountsAPI` has
  two read-only methods and no Resolve; `accounts:reveal` is granted to the
  accounts subject only. Modules resolve inside Go at the point of use.
- **`internal/git` is not `internal/checkpoint`.** checkpoint overrides
  `$GIT_DIR/info/attributes` to disable the project's `.gitattributes` so bytes
  round-trip untouched — right for a snapshot, silently wrong for a commit
  (`* text=auto`, `*.bin filter=lfs`). Never commit the user's work through it.
  What is shared is `Locate` and `internal/gitparse`.
- **Push is gated, commit is not — unless a `git-guard` says so.** Push leaves
  the machine; commit is local and reversible. A guard binds an env tag to a
  branch *pattern*, so it also covers branches that do not exist yet. There is
  no force push: no flag, no parameter, no code path.
- **An agent may push only to a branch recorded on a task it holds**
  (`task.branch`). A list living in the session dies with it; the task does not.
- **Closing a task is the one gate.** A board may declare a `verify` command;
  DevKit runs it and the exit code decides. An agent's own claim that tests
  passed is a sentence with nothing behind it — DevKit cannot see the tools it
  ran. A red result refuses and does **not** fall back to asking. Changing a
  board (workflow, verify, binding) is `tasks:admin` and UI-only, so an agent
  can trigger the command and never choose it. An agent may *create* a board
  (`tasks.board.create`, `tasks:write` `board:new`): it gets no verify command
  — its gate is the person's confirmation — and is bound only into one of the
  session's own workspaces (`tasks.Service.CreateBoardFor`). Runs are background, one per project (they share a work tree),
  queued durably in `task_verify_runs`; `Recover` puts an interrupted run back
  to `queued` and never infers a result, and a request older than 24h goes
  stale rather than grading code it was never made against.
- **Every rule in `tasks` reads a status's `category`, never its name** — there
  is a test with a board whose terminal column is called "Đã ship".
- **Auth in `apiclient` is an account binding, not a string.** Rotation is a
  one-place edit and Copy as cURL emits `$DEVKIT_TOKEN`. The redirect policy
  compares the whole **origin**: comparing hostnames passed under httptest,
  where both servers are 127.0.0.1, and the test caught the credential
  following a cross-service redirect. A blocked redirect comes back as the 3xx
  itself (`redirectBlocked` names where it pointed), not as an error.
- **API client variables are Postman's three scopes**: globals (`api-globals`,
  one item; two devices' copies merge and fold back into one on the next edit)
  < collection, root folder first (both halves in the `api-collection` sealed
  payload — its metadata keeps the old shape, because an older DevKit validates
  metadata field by field and would refuse the whole pull) < environment.
  `api-globals` is a new item type, so an older DevKit on another device stops
  pulling once one exists, like every item type added before it. Secrets are write-only everywhere: an update names the
  secrets to KEEP (`VarsUpdate.KeepSecrets`) and only new values cross the
  bridge. Send still requires an environment — its tag is the gate — even when
  every variable comes from elsewhere.
- **A request is edited, not just sent.** `RequestInput` (no timestamps) is what
  the bridge takes; query/headers/form are ordered `KVList` rows with `disabled`
  (a stored map still decodes), `:name` path segments are `pathVars` on the
  request, and `bodyMode` sets Content-Type unless a header row does.
  `ApiSendRequest` sends the editor's unsaved state; `ApiCancelSend` stops it.
  Plain `http://` is allowed to loopback always, elsewhere only with no bound
  account on a tag that does not ask first (`ErrPlainHTTP`). Pasting curl
  (`ParseCurl`) opens a draft; it is not an import.
- **Import/export is format-agnostic** (`internal/apiclient/interchange.go`).
  Every format converts to or from one tool-neutral `Bundle` and never touches
  the vault; `Service.Import` / `Service.Export` are the only code that turns a
  Bundle into vault items or back, so the rules live once: no secret VALUE
  leaves (names do, empty), an account binding leaves as placeholder variables
  (`{{<account>_token}}`, named via the optional `AccountNamer`), an imported
  environment needs a tag (the person's choice, else one the file carries that
  this machine knows — else `ErrTagRequired`, before anything is written).
  **Adding a format is a new `format_*.go` implementing `Decoder` and/or
  `Encoder`, plus a line in `formats`**; it may not reach into the model — a
  missing concept grows the Bundle tool-neutrally. Today: `postman` (read +
  write; an environment's tag rides in its name as `Name [tag]`) and `curl`
  (write: a bash script, one function per request, `set -u`, running the file
  alone sends nothing). The window lists formats from `ApiFormats` and knows
  none by name. Export is `api:read` scope `export:*`, UI only — no MCP tool.
  Agents get `api.sendAdhoc`, and every request/response they read has
  credential-named rows masked (`Request.ForAgent`). An ad-hoc send
  (`SendInput.Adhoc`) that carries a bound account or a secret variable asks
  the person every time, on any tag: the agent chose the host, so the tag
  cannot have answered "send this token there".
- **Agents build the API client's contents, through the person**
  (`internal/mcp/tools_apiclient_write.go`): `api.listCollections`,
  `api.{create,update,delete}Collection`, `api.saveRequest`, `api.deleteRequest`,
  `api.{create,update}Environment`. Every write is `confirmAPIWrite` (kind `api`,
  inside the capability check). It asks EVERY time, no allow_always, for an
  environment (it names the hosts and its tag decides whether sending asks), a
  request that `Service.Carries` a credential (bound account, a secret variable under
  any environment, a credential typed into a row — `api.send` fires a saved request on
  the tag's rule alone), a collection's variables or parent, and any delete. Other
  writes offer allow_always. Variable edits are set/remove, the rest kept; a masked
  row sent back keeps the stored value (`Service.RestoreMasked`), and a masked value
  with nothing to keep is refused. Deleting an environment and Globals stay UI-only.
  Every write emits `apiclient:changed` (`Service.OnChanged`, via a wrapped item
  store), which the sidebar follows.
- **Boards bind to workspaces through local `task_board_bindings`** (not synced):
  each machine maintains its own binding of board → workspace, never written into
  the synced board metadata. `workspace_id` is nullable; a row's mere presence
  means "this machine has considered the board", distinguishing never-considered
  from deliberately-unbound. Agents see only boards bound to workspaces in their
  session's selected `workspace_id`, getting `tasks.notFound`
  otherwise—no second-guessing, and no MCP tool may bind. `project_path` is
  read-only fallback for legacy boards; `verify.service` names a workspace service
  instead, traveling through sync unlike paths. `Considered` marker guards
  `MigrateBoardBindings` to avoid re-binding unbound boards on restart.
- **Git over SSH uses the account's key, served by `internal/gitssh`** (a leaf). An `ssh-key`
  account holds the private key and its passphrase packed in one sealed secret; `gitssh.Open`
  serves it from a private in-process agent (0700 socket dir, one key, gone with the command) and
  git runs with `GIT_SSH_COMMAND` pointing at it with `IdentitiesOnly=yes`. `ssh-key` accounts are
  **local by default**: a scoped row whose owner is `local:<id>` (never swept as an orphan, resolved
  by id, never offered to the API client or revealed again), `Promote` is how one starts syncing.
  `accounts.ForRemote(host, owner, env, via)` picks the account for an unbound remote — keys over
  SSH, tokens over HTTPS — and remembers it per `(host, owner, via)`; the app's `remotePicker` asks
  the person through `interaction.Hub.SelectAccount` when two fit. SSH aliases resolve through
  `ssh -G`. A key that cannot be opened stops the command; it never falls back to the machine's own
  key. `~/.ssh/config` `IdentityFile` entries are still tried *after* a refused account key (see the
  pinned test). `internal/git` tests that touch SSH use `code.example.test`, not `github.com`: the
  cloud sandbox injects a `url.insteadOf` that rewrites github.com SSH URLs to HTTPS.
- **Any credential kind can be machine-local, not only keys.** `CreateInput.Local` makes the same
  scoped `local:<id>` row for a token or basic credential (never for `external`, or with an env
  restriction — the row has nowhere to keep one). `Verify` handles local rows (`verifyLocal`): the
  subject and verdict are kept in the row, scopes and expiry are shown once and not stored.
  `Service.CreateGitToken` is the git entry point (GitHub/GitLab token, Bitbucket basic with a
  required username; the provider must have an `SSHHost`, which is how a repository host is
  marked). `accountResolver.ResolveForRemote` gives git a stand-in username (the verified login,
  else `git`) when a token has none; the API client still gets what is stored.
- **Rotating a token is in place** (`Service.RotateToken`, bridge `AccountsRotateToken`): the id stays so
  bindings, remembered choices and the commit identity survive; the username is kept unless a new one is
  given (read with `Reveal`, since `Resolve` with no env refuses an env-restricted account); only
  `token`/`basic` accounts that are shared or machine-local can be rotated (a key is replaced by adding
  the new one; a connection's password changes through its profile). `Rotate` now clears subject, scopes
  and expiry (shared) or the local row's verification: they describe the OLD value.
- **A key and its API token are two accounts the person can say belong together** (`internal/accounts/links.go`, table
  `account_links` migration 44, bridge `AccountsLinkToken|UnlinkToken|Rename`, frontend `modules/accounts/EditAccountDialog.tsx`,
  `accountGroups.ts`, gallery `?gallery#accounts-links`). A GitLab/GitHub **SSH key moves the code, a token reads the pull
  requests**; to the person that is one login, to DevKit two credentials (a key is no bearer; a token cannot sign git), each with
  its own value, verification and rotation — so they stay two accounts and a *link* is a declaration: `LinkToken(keyID, tokenID)`
  takes an `ssh-key` and a `token` of the same provider (not a connection's login; `ErrLinkMismatch` otherwise), one token per key,
  one token may serve several keys. Never inferred: a token may be a bot's or another login's (those are simply separate accounts;
  the Edit dialog shows a warning when both are verified and their `subject`s differ). The table is **local** like identities (a
  shared account's metadata is decoded field by field by every DevKit, so a new field would make an older one refuse it); a link to
  an account this device does not have is not shown; `Delete` drops a link from either end and `Promote` (a local account taking a
  new id) moves it. `Account.LinkedTokenID` (on a key) and `LinkedKeyIDs` (on a token) come with `List`/`Get`.
  **The pull-request card uses it**: `prCredentials.Token` takes the token linked to the key a remote is *bound* to before it guesses a
  token by host (so two tokens on a host are no longer "ambiguous" for that remote); a key with no linked token and no token on the
  host is still the key-only notice, which now carries the key's id (`Item.KeyAccountID`, `KeyOnlyError.AccountID`) so the card's
  "Add token" opens Accounts' Add token **for that key** (`stores/addTokenRequest.ts`: provider + key id): the provider is the
  key's, the token is named after it and attached once saved, and no commit identity is asked (a token commits as its key does).
  The Accounts list draws a linked token under its key (`groupAccounts`); the pencil opens **Edit account**: rename (an external
  sign-in and a connection's password cannot be renamed; the id stays, so bindings, choices and links keep working — an envelope is
  sealed against the id, not the label), and for a key: attach an existing unlinked token of the provider, add a new one inline
  ("Add and attach": kept on this machine, verified, linked), replace its token, detach it (the token account stays); for a token: the
  keys it serves, each detachable. Link, unlink and rename fire `accounts:changed` like every account write. **Not done**: environment
  restrictions are still not editable after creation; a link does not sync (it is how this machine groups its accounts); linking a
  Bitbucket key (DevKit reads no Bitbucket pull requests); the scope hint in Add token still names only `write_repository` for GitLab
  (a token that reads merge requests needs `read_api`). Not seen in the native app — the dialog's states were rendered from the
  gallery with the Browser pane hidden (text and structure read, no screenshot).
- **Importing a key from disk is UI-only and path-guarded** (`internal/bridge/accounts_sshimport.go`).
  `gitssh.Discover` lists `~/.ssh` one level deep (no symlinks, ≤64 KB, skips `config`/`known_hosts`/`*.pub`)
  and `ReadAliases` reads `Host`/`IdentityFile`/`Include` as TEXT (`ssh -G` would report default key
  files for every host). The bridge reads a path only if this window was first shown it — listed by
  `AccountsDiscoverSSHKeys` or returned by the native picker — and the whole set of methods answers
  "desktop app only" in server mode (an unauthenticated bridge must not read files by path). No MCP
  tool exists for it. Keys are deduplicated by `(provider, fingerprint)`: one key may serve GitHub and
  GitLab, never one provider twice (`gitssh.QuickInfo` reads an OpenSSH key's public half without the
  passphrase, so listing and dedupe never run the slow KDF). An ssh alias bound to a key account
  (`ChooseForAlias`, stored in `account_remote_choices` as `(alias, "*", ssh)`) answers before the
  repository owner is asked.
- **Commits are written as the account's identity.** An account may carry a name and email
  (`account_identities`, local — not in the account metadata, whose strict decoder would make an
  older DevKit refuse the record). `git.Service.CommitIdentity` decides who the next commit is:
  the repository's own `user.email` wins, else the account of the branch's upstream remote (then
  `origin`, then the only remote) when it has BOTH halves, else git's configuration. It looks the
  account up through `remotePicker.IdentityFor`, which never opens a dialog. `Commit` passes the
  identity as `GIT_AUTHOR_*`/`GIT_COMMITTER_*` (an amend keeps the old author) and never writes
  `.git/config`. Agents commit through the same path, so their commits carry it too.
- **`internal/notify` and `internal/gitparse` are leaves** pinned by
  `internal/arch`, extracted as their own commits before the modules that
  needed them. `notify.Target` is three strings and deliberately not the
  frontend's `EntityRef`.
- **Regenerating bindings**: `wails3` may not be on PATH. Install it into a
  throwaway module (`go install github.com/wailsapp/wails/v3/cmd/wails3@<the
  version in go.mod>` from a temp dir) rather than into this one, then
  `wails3 generate bindings -clean=true -ts -i` — the `-ts` matters, the
  default output is `.js` and this repo commits `.ts`.

