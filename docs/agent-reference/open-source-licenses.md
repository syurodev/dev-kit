### Open source licenses (`THIRD_PARTY_LICENSES.md`)
The notices for everything bundled into DevKit live in `THIRD_PARTY_LICENSES.md` at the repository root and in
Settings → Personal → "Open source licenses" (`settings/LicensesSetting.tsx`, fetched on demand as an emitted
`?url` asset, shown as plain text by `thirdPartyLicenses.ts`). The file is **generated**, never edited:
`bun scripts/third-party-licenses.mjs` reads the Go module cache (app, all three OSes, GUI and `-tags server`; the
`analyzer-ts` module), `frontend/node_modules` (the closure of `dependencies`), the Java analyzer's runtime jars
(`mvn dependency:list` + `~/.m2`) and the Inter font. The file is one entry per package (name, version, SPDX id,
source, note, the authors' own text, NOTICE files included), the way attribution notices are usually written. A package
offered under several licenses says which one DevKit uses (Apache-2.0 first, then MIT); a package that ships no license
file gets its text from its repository (cached in `scripts/third-party-licenses.fetched.json`, so the output is
reproducible offline); one that publishes none anywhere (`use-composed-ref`) says so. `--check` fails when the file is
stale; it needs those toolchains installed, so it is not a CI step. Re-run it after changing any dependency (`go.mod`, `package.json`,
`analyzer/pom.xml`, `analyzer-ts/go.mod`) or a bundled font. The Claude / Codex runtimes are downloaded at run
time, not bundled, and are not listed.
- Product identity (name `DevKit`, company `SynX`, id `com.syuro.devkit`) is in `build/config.yml` and the
  generated build assets under `build/` (plists, `desktop`, `info.json`, NSIS, MSIX). The macOS menu-bar name is
  `CFBundleName` in `Info.plist` / `Info.dev.plist`. `build/ios/` is scaffolding and still carries the template name.
- `build/linux/nfpm/nfpm.yaml` still says `license: "MIT"` from the template; DevKit has no LICENSE of its own yet,
  so that field is a placeholder to settle with the project's own license.

