// Package forge is DevKit's view of a code host: the pull request (GitHub) or
// merge request (GitLab) a branch became, its checks and its review comments.
//
// It is a leaf. Nothing here knows about sessions, accounts, git work trees or
// the vault: a caller hands it a host, a repository path and a token, and gets
// back data that does not say which platform it came from. That is what lets
// GitLab plug in behind Provider without the UI or the stored shape changing —
// the vocabulary is "change request", "check" and "comment", and a Terms value
// carries the words a person sees ("PR" or "MR").
//
// Every read goes through a conditional-request cache (ETag / If-None-Match):
// a 304 costs no rate limit on GitHub, so polling an unchanged change request
// is nearly free.
package forge
