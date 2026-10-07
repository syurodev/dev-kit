// Package updater answers one question: is there a newer DevKit release than
// the one running? It tells the person and nothing more. It downloads no
// binary and installs nothing; the person follows the link and installs the
// release the way they installed this one.
//
// Releases come in two kinds: vX.Y.Z, and vX.Y.Z-beta, which is published as a
// GitHub pre-release. Which of them a person is offered depends on what they
// run: a stable build is only ever offered a stable release, and a beta build
// is offered the newest of either, so someone testing betas hears about the
// next beta and about the stable release that supersedes it.
//
// It is a leaf. The running version is a parameter, not an import, so the
// package can be tested against any version and does not know how the linker
// stamps one.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultRepo is the repository whose releases the app publishes to.
const DefaultRepo = "syurodev/dev-kit"

const (
	defaultAPIBase = "https://api.github.com"
	requestTimeout = 10 * time.Second
	// maxResponseBytes bounds what is read from the API. A release document is
	// a few kilobytes; the cap only matters against a server that never stops.
	maxResponseBytes = 1 << 20
)

var (
	// ErrDevBuild means the running binary carries no release version, so
	// there is nothing to compare. It is a state, not a failure.
	ErrDevBuild = errors.New("this build has no release version")
	// ErrNoRelease means the repository has no published release (or none this
	// client may see, which GitHub reports the same way).
	ErrNoRelease = errors.New("no release has been published")
)

// Result is the outcome of one check.
type Result struct {
	// Current is the running version as given, "v" stripped.
	Current string `json:"current"`
	// Latest is the newest published release's version, "v" stripped.
	Latest string `json:"latest"`
	// Available is true only when Latest is strictly newer than Current.
	Available bool `json:"available"`
	// Prerelease is true when Latest is a beta. It can only be true for a
	// person already running one.
	Prerelease bool `json:"prerelease"`
	// URL is the release page, always on github.com under the repository
	// checked. Empty when no update is available.
	URL string `json:"url"`
	// PublishedAt is when the release was published, RFC 3339, or "".
	PublishedAt string `json:"publishedAt"`
}

// Checker reads the latest release from GitHub.
type Checker struct {
	// Repo is "owner/name". Empty means DefaultRepo.
	Repo string
	// APIBase is the GitHub API root. Empty means api.github.com; tests point
	// it at a local server.
	APIBase string
	// Client performs the request. Nil means a client with a short timeout.
	Client *http.Client
}

type releaseDoc struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

// Check compares current with the newest release on its channel (see the
// package comment). "Newest" is by version precedence, not by publish date: a
// patch release of an older line published after a newer line must not be
// mistaken for the latest.
func (c *Checker) Check(ctx context.Context, current string) (Result, error) {
	current = strings.TrimPrefix(strings.TrimSpace(current), "v")
	if current == "" || current == "dev" {
		return Result{}, ErrDevBuild
	}
	have, ok := parseVersion(current)
	if !ok {
		return Result{}, fmt.Errorf("running version %q is not a release version", current)
	}

	repo := c.repo()
	releases, err := c.list(ctx, repo)
	if err != nil {
		return Result{}, err
	}

	// A beta build follows betas; anything else follows stable releases only.
	betaChannel := have.pre != ""
	var (
		best        releaseDoc
		bestVersion version
		found       bool
	)
	for _, release := range releases {
		if release.Draft {
			continue
		}
		v, ok := parseVersion(release.TagName)
		if !ok {
			continue // a tag that is not a version is not a release of the app
		}
		if !betaChannel && (release.Prerelease || v.pre != "") {
			continue
		}
		if !found || v.newerThan(bestVersion) {
			best, bestVersion, found = release, v, true
		}
	}
	if !found {
		return Result{}, ErrNoRelease
	}

	result := Result{
		Current:     current,
		Latest:      strings.TrimPrefix(strings.TrimSpace(best.TagName), "v"),
		Prerelease:  best.Prerelease || bestVersion.pre != "",
		PublishedAt: best.PublishedAt,
	}
	if bestVersion.newerThan(have) {
		page, ok := releasePage(best.HTMLURL, repo)
		if !ok {
			// Telling the person an update exists while handing them a link to
			// somewhere else is worse than not telling them.
			return Result{}, fmt.Errorf("release page %q is not on github.com/%s", best.HTMLURL, repo)
		}
		result.Available = true
		result.URL = page
	}
	return result, nil
}

func (c *Checker) repo() string {
	if r := strings.Trim(strings.TrimSpace(c.Repo), "/"); r != "" {
		return r
	}
	return DefaultRepo
}

// list reads the repository's most recent releases. A page of 30 is far more
// than the handful published between two versions a person might skip.
func (c *Checker) list(ctx context.Context, repo string) ([]releaseDoc, error) {
	base := strings.TrimRight(c.APIBase, "/")
	if base == "" {
		base = defaultAPIBase
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/repos/"+repo+"/releases?per_page=30", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "dev-kit-update-check")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach the release server: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// A repository this client may not see is reported the same way as one
		// with nothing in it.
		return nil, ErrNoRelease
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("the release server is rate limiting this machine; try again later")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("the release server answered %s", resp.Status)
	}

	var releases []releaseDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("the release server's answer could not be read: %w", err)
	}
	return releases, nil
}

// releasePage returns raw when it is an https page of repo's own releases on
// github.com. The value comes off the network, and the UI opens it in the
// person's browser, so it is checked rather than trusted.
func releasePage(raw, repo string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
		return "", false
	}
	prefix := "/" + repo + "/releases/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", false
	}
	u.RawQuery, u.Fragment = "", ""
	return u.String(), true
}

// version is the numeric core of a semantic version plus whether it carries a
// prerelease tag. Build metadata ("+abc") is ignored, as semver says.
type version struct {
	major, minor, patch uint64
	pre                 string
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var v version
	if i := strings.IndexByte(s, '-'); i >= 0 {
		v.pre = s[i+1:]
		s = s[:i]
		if v.pre == "" {
			return version{}, false
		}
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	nums := [3]*uint64{&v.major, &v.minor, &v.patch}
	for i, part := range parts {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || (len(part) > 1 && part[0] == '0') {
			return version{}, false
		}
		*nums[i] = n
	}
	return v, true
}

// newerThan orders versions by precedence. A prerelease sorts below its own
// release (1.0.0-rc.1 < 1.0.0), so a person running a release candidate is
// offered the final version. Prerelease tags are compared as semver defines:
// dot-separated identifiers, numeric ones numerically and below alphanumeric.
func (v version) newerThan(o version) bool {
	switch {
	case v.major != o.major:
		return v.major > o.major
	case v.minor != o.minor:
		return v.minor > o.minor
	case v.patch != o.patch:
		return v.patch > o.patch
	}
	switch {
	case v.pre == o.pre:
		return false
	case v.pre == "":
		return true
	case o.pre == "":
		return false
	}
	return comparePre(v.pre, o.pre) > 0
}

func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aErr := strconv.ParseUint(as[i], 10, 64)
		bn, bErr := strconv.ParseUint(bs[i], 10, 64)
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				if an > bn {
					return 1
				}
				return -1
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) > len(bs):
		return 1
	case len(as) < len(bs):
		return -1
	}
	return 0
}
