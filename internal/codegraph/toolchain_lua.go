// internal/codegraph/toolchain_lua.go
package codegraph

// Everything CodeGraph knows that is specific to Lua as a PROJECT lives here:
// what makes a folder one, and that its analyzer needs nothing installed.
// How the analysis itself works is analyzer_lua*.go.
//
// The shape is the same as toolchain_java.go's on purpose — a language is a
// Toolchain value and a line in DefaultToolchains — with one field that Java's
// cannot have: Native. Java's analyzer is a jar that has to be built or
// shipped and launched by a JDK; Lua's is a function call in this process, so
// there is no artifact to find, no requirement to probe and no setup panel to
// show. See Toolchain.Native for why that is worth a field of its own rather
// than a second Analyzer registry beside the toolchains.

// LuaToolchain describes the Lua analyzer.
//
// It takes no repoRoot, unlike JavaToolchain: there is no source to build and
// no artifact to search for. Everything it needs is compiled into DevKit.
func LuaToolchain() Toolchain {
	return Toolchain{
		Language: "lua",
		Label:    "Lua",
		Native:   NewLuaAnalyzer,
		// What marks a module root. The rockspec is Lua's build file and the
		// direct equivalent of a pom.xml; the linter and formatter configs are
		// in because plenty of real Lua projects — Neovim plugins, Kong
		// plugins vendored into a larger repository — have no rockspec at all
		// and one of these is the only file that says "a project starts here".
		//
		// handler.lua is the odd one, and it earns its place: it is the file
		// Kong requires a plugin to be named after, so a repository holding
		// several plugins registers as one module per plugin instead of as a
		// single root — exactly what Markers is for. A repository holding one
		// plugin still reads as one module.
		Markers: []string{
			"*.rockspec",
			".luacheckrc", ".luacov", ".busted",
			"stylua.toml", ".stylua.toml", "selene.toml",
			"handler.lua",
		},
		// The fallback that matters more here than it does for Java: a folder
		// of .lua files with no config of any kind is the normal shape of a
		// Neovim config and of a plugin someone is still writing.
		SourceExt: ".lua",
	}
}
