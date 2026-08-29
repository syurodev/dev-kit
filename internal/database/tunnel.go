package database

import (
	"context"
	"errors"
)

// ErrSSHProfileMissing means a profile is configured to reach its database
// through an SSH profile that no longer exists.
//
// DevKit fails here rather than connecting directly. When a tunnel is in play,
// Host/Port on the profile are addresses as seen from the SSH server — usually
// localhost or an internal IP. Dialling that pair from this machine is not the
// same destination reached slowly; it is a different machine on a different
// network, so a silent fallback would send connection attempts somewhere they
// do not belong.
var ErrSSHProfileMissing = errors.New("the SSH profile for this tunnel no longer exists")

// ErrTunnelKeyNeedsPassphrase means the SSH profile's private key is encrypted.
// v1 does not support it: the tunnel opens lazily inside PoolManager.Get, where
// there is nobody to prompt.
var ErrTunnelKeyNeedsPassphrase = errors.New("this SSH profile's key needs a passphrase, which DevKit cannot ask for while opening a tunnel")

// TunnelSpec asks for one local (-L) forward to a database.
type TunnelSpec struct {
	SSHProfileID string
	TargetHost   string
	TargetPort   int
}

// TunnelHandle is an open forward. DevKit dials LocalHost:LocalPort instead of
// the profile's own host and port.
type TunnelHandle struct {
	ID        string
	LocalHost string
	LocalPort int
}

// Tunnel opens and closes local port forwards. It is declared here, narrow and
// free of SSH types, so internal/database never imports internal/ssh — the same
// "accept interfaces" boundary EnvLookup and Store already draw.
type Tunnel interface {
	Open(ctx context.Context, spec TunnelSpec) (TunnelHandle, error)
	Close(id string) error
}
