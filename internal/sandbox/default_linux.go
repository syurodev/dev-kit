//go:build linux

package sandbox

func defaultBackend() Backend {
	return NewBwrapBackend()
}
