// internal/lsp/client.go
package lsp

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
)

// The six requests an index is built from, typed.
//
// Everything here is a thin wrapper over Conn.Call. It is worth the file
// anyway: each of these has one detail that is easy to get wrong and
// impossible to notice — a definition answered in two different shapes, a
// call-hierarchy item whose opaque Data has to survive the round trip, a
// position counted in UTF-16.

// Initialize performs the handshake and returns once the server is ready for
// requests. rootURI is the project the server should load.
func (c *Conn) Initialize(ctx context.Context, rootURI string) error {
	params := map[string]any{
		"processId": nil,
		"rootUri":   rootURI,
		"workspaceFolders": []any{
			map[string]any{"uri": rootURI, "name": filepath.Base(strings.TrimPrefix(rootURI, "file://"))},
		},
		"capabilities": map[string]any{
			// Declared false on purpose. Dynamic registration would have the
			// server send requests back asking this client to register
			// capabilities it does not implement, and a configuration request
			// it cannot answer usefully.
			"workspace": map[string]any{
				"configuration":          false,
				"workspaceFolders":       true,
				"didChangeConfiguration": map[string]any{"dynamicRegistration": false},
				"didChangeWatchedFiles":  map[string]any{"dynamicRegistration": false},
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"dynamicRegistration": false},
				"documentSymbol": map[string]any{
					"dynamicRegistration":               false,
					"hierarchicalDocumentSymbolSupport": true,
				},
				"callHierarchy": map[string]any{"dynamicRegistration": false},
				"definition":    map[string]any{"dynamicRegistration": false, "linkSupport": true},
			},
			"general": map[string]any{
				// Asking for UTF-8 does not mean getting it; every position
				// this client builds is converted to UTF-16 regardless, which
				// is the encoding the protocol defaults to.
				"positionEncodings": []string{"utf-16"},
			},
		},
	}
	if err := c.Call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	return c.Notify("initialized", map[string]any{})
}

// DidOpen tells the server about a file's contents. An indexer opens every
// file it wants an answer about and never closes one: closing would let the
// server drop the parse it is about to be asked for again.
func (c *Conn) DidOpen(uri, languageID, text string) error {
	return c.Notify("textDocument/didOpen", map[string]any{
		"textDocument": TextDocumentItem{URI: uri, LanguageID: languageID, Version: 1, Text: text},
	})
}

// DocumentSymbol returns what a file declares, nested.
func (c *Conn) DocumentSymbol(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	var symbols []DocumentSymbol
	err := c.Call(ctx, "textDocument/documentSymbol",
		map[string]any{"textDocument": TextDocumentIdentifier{URI: uri}}, &symbols)
	return symbols, err
}

// PrepareCallHierarchy resolves a position to the callable that lives there.
// It is the handle OutgoingCalls needs, and it is why nodes have to carry the
// selection range the server gave them rather than a position of our own.
func (c *Conn) PrepareCallHierarchy(ctx context.Context, uri string, at Position) ([]CallHierarchyItem, error) {
	var items []CallHierarchyItem
	err := c.Call(ctx, "textDocument/prepareCallHierarchy", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     at,
	}, &items)
	return items, err
}

// OutgoingCalls is "what does this call". The item goes back exactly as it
// came, opaque Data and all.
func (c *Conn) OutgoingCalls(ctx context.Context, item CallHierarchyItem) ([]CallHierarchyOutgoingCall, error) {
	var calls []CallHierarchyOutgoingCall
	err := c.Call(ctx, "callHierarchy/outgoingCalls", map[string]any{"item": item}, &calls)
	return calls, err
}

// Definition is "where is this declared". Used on an import's module
// specifier, which is how module resolution is answered by the thing that
// actually implements it — tsconfig paths, package exports, extension
// inference and all — rather than reimplemented here and got subtly wrong.
//
// The protocol allows three shapes for the answer (one Location, many
// Locations, or LocationLinks) and servers use all three, so the decode tries
// each rather than assuming.
func (c *Conn) Definition(ctx context.Context, uri string, at Position) ([]Location, error) {
	var raw json.RawMessage
	err := c.Call(ctx, "textDocument/definition", TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     at,
	}, &raw)
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	return decodeDefinition(raw), nil
}

func decodeDefinition(raw json.RawMessage) []Location {
	var locations []Location
	if err := json.Unmarshal(raw, &locations); err == nil && len(locations) > 0 && locations[0].URI != "" {
		return locations
	}
	var links []locationLink
	if err := json.Unmarshal(raw, &links); err == nil && len(links) > 0 && links[0].TargetURI != "" {
		out := make([]Location, 0, len(links))
		for _, link := range links {
			out = append(out, Location{URI: link.TargetURI, Range: link.TargetRange})
		}
		return out
	}
	var single Location
	if err := json.Unmarshal(raw, &single); err == nil && single.URI != "" {
		return []Location{single}
	}
	return nil
}

// URIFor is the file:// URI for a path, with the escaping a server is entitled
// to expect — a project checked out under a directory with a space or a '#' in
// it is not exotic.
func URIFor(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		// A Windows path: file:///C:/...
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path}
	return u.String()
}

// PathFor is URIFor in reverse, for turning a server's answer back into a path
// this process can compare against its own file list.
func PathFor(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	path := u.Path
	// file:///C:/x parses with a leading slash that is not part of the path.
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// UTF16Offset converts a byte offset within one line into the character offset
// the protocol counts in.
func UTF16Offset(line string, byteOffset int) int { return utf16Offset(line, byteOffset) }
