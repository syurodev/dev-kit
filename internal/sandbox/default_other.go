//go:build !linux && !darwin

package sandbox

func defaultBackend() Backend {
	return &unavailableBackend{}
}
