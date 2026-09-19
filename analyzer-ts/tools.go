//go:build tools

// Package tools pins the TypeScript analyzer DevKit builds and ships.
//
// A module of its own, not a dependency of DevKit's: tsgo is a program we
// build, never a library we import (everything inside typescript-go lives
// under internal/), and putting it in the root go.mod would drag its whole
// dependency tree into the app's build for a binary that is spawned, not
// linked.
package tools

import _ "github.com/microsoft/typescript-go/cmd/tsgo"
