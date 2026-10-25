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

