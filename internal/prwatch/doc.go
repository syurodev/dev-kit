// Package prwatch follows the pull request (or merge request) a session's
// branch became, and is the one place that turns "a folder and a branch" into
// "a repository on a host, an account to call it as, and the change request".
//
// Three callers stand on it: the summary card (State, polled by the window
// while the session is on screen), the MCP pr.* tools (Open, which hands back
// a Handle), and the bridge (Attach, for a pull request found by hand).
//
// It knows nothing about git, accounts or the interaction hub: Source says
// where a folder stands, Credentials hands over a token for one call, Store
// remembers hand-attached requests. The composition root wires them, so this
// package imports only forge and the arch test keeps it that way.
//
// Polling is the window's: State answers "what is true now, and when should I
// ask again", and answers from cache when asked twice within a few seconds, so
// two windows or two sessions on one branch cost one request. A change request
// that is merged or closed is never asked about again.
package prwatch
