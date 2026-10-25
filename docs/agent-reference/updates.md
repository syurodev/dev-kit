### Update notice (`internal/updater`, `internal/buildinfo`)
DevKit **tells** you a newer release exists; it downloads and installs nothing (OTA install
is a later step). `internal/updater` is a leaf that lists `syurodev/dev-kit` GitHub releases;
`buildinfo.Version` is stamped by the packaging build (`-X devkit/internal/buildinfo.Version`,
from `DEVKIT_VERSION`, which `build-prod.yml` sets from the tag). Unstamped builds report
`dev` and never reach the network.

- **Two release kinds, two channels.** `vX.Y.Z` is stable; `vX.Y.Z-beta` is a GitHub
  pre-release. A stable build is offered only stable releases; a beta build is offered the
  highest of either, so a beta tester hears about the next beta and the stable that supersedes
  it. "Newest" is by version precedence (`1.0.0-beta < 1.0.0`), never publish date, and the tag
  suffix decides even if the pre-release flag was forgotten. `/releases/latest` is not used
  because it never returns pre-releases.
- The release page URL comes off the network and is opened in the browser, so it must be
  `https://github.com/<repo>/releases/...` or the check fails rather than linking elsewhere.
- Bridge: `UpdatesCheck(automatic)`, `UpdatesCurrentVersion`, `PreferencesGet/SetUpdateChecks`
  (key `update_checks`, default on). Only the automatic startup check honours the preference
  (enforced in Go); "Check now" always answers. UI: `UpdateNotifier` (one toast per release,
  remembered in localStorage) and Settings → Account → Updates.
- Test: `go test ./internal/updater/ ./internal/bridge/ -run Update`; try a stamped build with
  `DEVKIT_VERSION=0.0.1 task build`.

