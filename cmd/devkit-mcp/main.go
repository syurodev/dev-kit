package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"devkit/internal/mcp"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			// Task 10: write agent MCP config. Stub for now.
			fmt.Fprintln(os.Stderr, "install: not implemented yet")
			os.Exit(1)
		}
	}
	if err := runProxy(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func runProxy() error {
	path, err := mcp.SocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("DevKit is not running or MCP host is unavailable")
	}
	defer conn.Close()

	errCh := make(chan error, 2)
	var once sync.Once
	closeConn := func() {
		once.Do(func() { _ = conn.Close() })
	}

	go func() {
		_, copyErr := io.Copy(conn, os.Stdin)
		closeConn()
		errCh <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(os.Stdout, conn)
		errCh <- copyErr
	}()

	// Wait for either direction to finish (EOF / disconnect).
	err = <-errCh
	closeConn()
	<-errCh
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}
