package app

import (
	"context"
	"errors"

	"devkit/internal/database"
	sshservice "devkit/internal/ssh"
	"devkit/internal/vaultitem"
)

// sshTunnel adapts the SSH service to the narrow port-forward interface the
// database pool declares, so internal/database never imports internal/ssh.
type sshTunnel struct {
	ssh *sshservice.Service
}

func (t sshTunnel) Open(ctx context.Context, spec database.TunnelSpec) (database.TunnelHandle, error) {
	// Local (-L) only, loopback bind, port 0 so the OS picks one — several
	// profiles can tunnel at once without colliding.
	info, err := t.ssh.StartForward(ctx, spec.SSHProfileID, sshservice.ForwardSpec{
		Direction:  sshservice.ForwardDirectionLocal,
		ListenHost: "127.0.0.1",
		ListenPort: 0,
		TargetHost: spec.TargetHost,
		TargetPort: spec.TargetPort,
	}, "")
	if err != nil {
		switch {
		case errors.Is(err, vaultitem.ErrNotFound):
			return database.TunnelHandle{}, database.ErrSSHProfileMissing
		case errors.Is(err, sshservice.ErrKeyPassphraseRequired):
			return database.TunnelHandle{}, database.ErrTunnelKeyNeedsPassphrase
		}
		return database.TunnelHandle{}, err
	}
	return database.TunnelHandle{ID: info.ID, LocalHost: info.ListenHost, LocalPort: info.ListenPort}, nil
}

func (t sshTunnel) Close(id string) error { return t.ssh.StopForward(id) }
