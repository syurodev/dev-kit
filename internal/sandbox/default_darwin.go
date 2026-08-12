//go:build darwin

package sandbox

func defaultBackend() Backend {
	return NewSeatbeltBackend()
}
