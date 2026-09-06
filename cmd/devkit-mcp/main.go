package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"devkit/internal/mcp"
	"devkit/internal/mcp/install"
)

func main() {
	args := os.Args[1:]
	// `go run ./cmd/... -- install ...` may pass a literal "--" through to the
	// program (depending on toolchain); strip it so Taskfile invocations work.
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		switch args[0] {
		case "hook":
			if len(args) < 2 || args[1] != "pretooluse" {
				fmt.Fprintln(os.Stderr, "usage: devkit-mcp hook pretooluse")
				os.Exit(2)
			}
			if err := runPreToolUseHook(); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			return
		case "install", "uninstall":
			if err := runInstallCmd(args[0], args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			return
		}
	}
	fs := flag.NewFlagSet("devkit-mcp", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socket := fs.String("socket", "", "unix socket of the DevKit MCP host (default: the shared mcp.sock)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if err := runProxy(*socket); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func runInstallCmd(action string, args []string) error {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "agent id: claude-code|codex")
	scope := fs.String("scope", "", "install scope: global|project")
	project := fs.String("project", ".", "project directory (required for project scope)")
	command := fs.String("command", "", "absolute path to devkit-mcp (optional; resolved if empty)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *agent == "" || *scope == "" {
		return fmt.Errorf("usage: devkit-mcp %s --agent claude-code|codex --scope global|project [--project PATH]", action)
	}

	projectPath := *project
	if *scope == string(install.ScopeProject) {
		abs, err := filepath.Abs(projectPath)
		if err != nil {
			return err
		}
		projectPath = abs
	}

	req := install.InstallRequest{
		Agent:       install.Agent(*agent),
		Scope:       install.Scope(*scope),
		ProjectPath: projectPath,
		Command:     *command,
	}

	path, err := install.ConfigPath(req)
	if err != nil {
		return err
	}

	switch action {
	case "install":
		if req.Command == "" {
			cmd, err := install.ResolveCommand()
			if err != nil {
				return err
			}
			req.Command = cmd
		}
		if err := install.Install(req); err != nil {
			return err
		}
		fmt.Printf("installed %s MCP (%s/%s) -> %s\n", install.ServerName, req.Agent, req.Scope, path)
	case "uninstall":
		if err := install.Uninstall(req); err != nil {
			return err
		}
		fmt.Printf("uninstalled %s MCP (%s/%s) from %s\n", install.ServerName, req.Agent, req.Scope, path)
	}
	return nil
}

// runProxy pipes stdio to the DevKit MCP host. With --socket it targets the
// per-session socket Agent Hub handed the agent; without, the shared socket.
func runProxy(path string) error {
	if path == "" {
		shared, err := mcp.SocketPath()
		if err != nil {
			return err
		}
		path = shared
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
