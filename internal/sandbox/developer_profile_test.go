package sandbox

import (
	"strings"
	"testing"
)

func TestSeatbeltProfileSessionDevelopment(t *testing.T) {
	p := buildSeatbeltProfile(Policy{WorkDir: "/work", SessionTempDir: "/private/tmp/dk-12345678", DenyReadPaths: []string{"/secret"}})
	for _, rule := range []string{
		`(allow signal (target same-sandbox))`,
		`(allow process-info* (target same-sandbox))`,
		`(allow pseudo-tty)`,
		`(literal "/dev/ptmx")`,
		`(require-all (regex #"^/dev/ttys[0-9]+$") (extension "com.apple.sandbox.pty"))`,
		`(global-name "com.apple.system.opendirectoryd.libinfo")`,
		`(local unix-socket (subpath "/private/tmp/dk-12345678"))`,
		`(remote unix-socket (subpath "/private/tmp/dk-12345678"))`,
	} {
		if !strings.Contains(p, rule) {
			t.Errorf("missing %s", rule)
		}
	}
	deny := strings.Index(p, `(deny file-read* file-write* (regex #"^/private/tmp/dk-"))`)
	own := strings.Index(p, `(allow file-read* file-write* (subpath "/private/tmp/dk-12345678"))`)
	secret := strings.LastIndex(p, `(deny file-read* file-write*`)
	if deny < 0 || own <= deny || secret <= own || strings.LastIndex(p, "(allow ") > secret {
		t.Fatal("private temp exception must follow temp deny but precede credential denies", p)
	}
	if strings.Contains(p, "(allow network-outbound)") || strings.Contains(p, "(allow signal)") {
		t.Fatal("unscoped capability", p)
	}
	none := buildSeatbeltProfile(Policy{WorkDir: "/work"})
	if strings.Contains(none, "unix-socket (subpath") {
		t.Fatal("no session temp means no Unix listener grant")
	}
}
