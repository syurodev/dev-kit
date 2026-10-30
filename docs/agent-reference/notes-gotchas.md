### Notes / gotchas
- **Brand marks come from Simple Icons, one import each** (`frontend/src/components/brand-icon/BrandIcon.tsx`,
  `@icons-pack/react-simple-icons`). Add a brand to its `BRANDS` table with a deep import
  (`@icons-pack/react-simple-icons/icons/SiKong`), never from the package's barrel (~7000 modules in the dev server). A mark is one path on
  `currentColor`, decorative (`title=""`, `aria-hidden`); a caller's size/colour wins (a tree row sizes its svgs).
  Used for: Kong on the `plugin` entry points (`EntryPointsTab`), the git host on an account row (`github`/`gitlab`/`bitbucket`
  provider ids, `isBrand`), Kafka on a cluster row (its connection state is the mark's colour). Not for files (that is
  `components/file-icon`, Material Icon Theme with light/dark variants), not for the Database engine marks (Figma wants the
  vendor's colours there, `modules/database/icons`), and not for Claude/Codex (`VendorIcon`, from Figma; OpenAI has no Simple Icons mark).
  The artwork is CC0 but the marks are their owners' trademarks. A new dependency means regenerating `THIRD_PARTY_LICENSES.md`.
  Gallery: `?gallery#brand-icons`.
- Sync login: production uses `internal/cloudconfig` (pinned `GET /v1/desktop/config`, disk cache, refresh on reconnect). Set `DEVKIT_OIDC_*` to override remote/cache for local dev. Omit all OIDC env + stay offline with no cache for local-only vault/modules.
- Storage is a single SQLite file (`modernc.org/sqlite`, no cgo) opened with
  `MaxOpenConns(1)`; migrations run forward-only from `storage.Migrate`. Because of the
  single connection, migration `Up` funcs must **not** be wrapped in an outer transaction.
- `bin/`, `frontend/dist/`, and `frontend/node_modules/` are gitignored — don't commit them.
- `frontend/.npmrc` sets `minimum-release-age=10080`; honoured by bun/pnpm, ignored by npm.
- `wails3 generate bindings` is slow (~15s, scans all packages) and regenerates
  `frontend/bindings/`; re-run it after changing any bridge (`AppService`) method signature,
  then `bun run build`.

- **Frontend tests: never `toMatchObject`/`toEqual`/`toStrictEqual` an object that holds React elements or fibers**
  (a pane header with `status`/`actions` nodes, a rendered tree, anything with `_owner`). When the assertion fails
  Bun pretty-prints the diff of that object, follows the fiber graph and allocates until the machine is out of
  memory (2026-10-30: a failing `panelHeaders.test.tsx` took 1.5 GB in 40 s and, run unbounded next to other jobs,
  brought a 31 GB machine down). Compare the primitives (`title`, `hint`), and check identity with
  `expect(Object.is(a, b)).toBe(true)`. A test that renders through a real store or provider should also count
  its renders or `openPanel` calls and fail past a small bound, so a loop fails in seconds.
- **Run the whole frontend suite with `bun run test:safe`** (`frontend/scripts/test-safe.sh`) when the machine is
  shared with agents, JVMs or anything else heavy: it runs the suite as sequential chunks, each with a time limit
  (`TEST_TIMEOUT`, default 300 s) and, where `systemd-run --user` exists, a memory ceiling (`TEST_MEM_MAX`, default
  6G), and prints a summary that names a timed-out (124) or memory-killed (137) chunk. Pass paths to run only
  those (`bun run test:safe src/stores`). `bun test --isolate` (the `test` script, and what CI runs) is one
  unbounded process; the heaviest chunk, `src/modules/agenthub`, peaks near 1.5 GB.
