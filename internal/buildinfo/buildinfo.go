// Package buildinfo carries what the linker knows about this binary: the
// release version stamped in by the packaging build. It is a leaf so that
// anything may read it and nothing here depends on anything.
package buildinfo

// DevVersion is what an unstamped build reports. A `go build`, `task dev` or
// `task run` binary has no release behind it, so there is nothing to compare
// against a newer release and the update check stays quiet.
const DevVersion = "dev"

// Version is the release this binary was built from, without a leading "v"
// ("1.4.0"). The packaging build sets it:
//
//	-ldflags "-X devkit/internal/buildinfo.Version=1.4.0"
//
// It must stay a plain string variable: the linker can only overwrite those.
var Version = DevVersion
