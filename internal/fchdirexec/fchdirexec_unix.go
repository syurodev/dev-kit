//go:build unix

// Package fchdirexec is the helper mode that lets a Go program start a child in a
// directory it holds a descriptor of, with no path lookup.
//
// Go cannot do it: os/exec's Dir is a path, and the child changes into it before the
// extra files are installed, so a path such as /proc/self/fd/N would mean the
// parent's descriptor there (and exists on Linux only). So the child is started as
// this very binary with argv[0] set to Arg0, the directory as the first inherited
// descriptor (3), and the program to run and its arguments after it:
//
//	argv = [Arg0, <program>, <args...>]      fd 3 = the directory
//
// The helper does fchdir(3), closes 3 and execve(program). It replaces its own
// process, so the program is the process the parent started: its exit status, its
// signals and its cancellation (exec.CommandContext) are the program's own, with no
// extra process in between.
//
// This is a package of its own, with nothing but the standard library, for the
// order the runtime initialises packages in (by import path, each once its imports
// are done): the hook below must run before the application's heavy packages
// initialise, or every helper start would pay for them. There is no flag or
// subcommand to find: the mode is selected by argv[0], which only a caller that
// starts the binary under exactly that name can set, and it gives that caller
// nothing it did not have (it runs the program it is told to run, which anyone who
// can start this binary can already do).
package fchdirexec

import (
	"os"
	"sync"
	"syscall"
)

// Arg0 is argv[0] of a process that is to act as the helper.
const Arg0 = "devkit-git-fchdir"

// Exit codes of the helper itself, so its failures are told from the program's own.
const (
	ExitFchdir = 125
	ExitExec   = 127
)

func init() {
	if len(os.Args) > 0 && os.Args[0] == Arg0 {
		run()
	}
}

func run() {
	if len(os.Args) < 2 {
		os.Stderr.WriteString("devkit-git: no program to run\n")
		os.Exit(ExitExec)
	}
	if err := syscall.Fchdir(3); err != nil {
		os.Stderr.WriteString("devkit-git: fchdir: " + err.Error() + "\n")
		os.Exit(ExitFchdir)
	}
	_ = syscall.Close(3)
	err := syscall.Exec(os.Args[1], os.Args[1:], os.Environ())
	os.Stderr.WriteString("devkit-git: exec " + os.Args[1] + ": " + err.Error() + "\n")
	os.Exit(ExitExec)
}

var (
	once sync.Once
	self string
	err  error
)

// Self is the path of this binary, which the helper is started from.
func Self() (string, error) {
	once.Do(func() { self, err = os.Executable() })
	return self, err
}
