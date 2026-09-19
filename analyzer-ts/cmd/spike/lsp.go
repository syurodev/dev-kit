package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The other candidate architecture: drive tsgo's LSP instead of its API.
//
// It matters because the API's getSourceFile hands back a base64 binary AST in
// an encoding internal to typescript-go, and call edges need an AST — there is
// no "who does this function call" in a compiler API. The LSP has exactly that
// question as a standard method (callHierarchy/outgoingCalls) and a stable
// protocol, so if it is affordable it wins on every axis except round trips.
// This measures the round trips.

func (c *client) notify(method string, params any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

type lspRange struct {
	Start struct {
		Line      int `json:"line"`
		Character int `json:"character"`
	} `json:"start"`
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          lspRange         `json:"range"`
	SelectionRange lspRange         `json:"selectionRange"`
	Children       []documentSymbol `json:"children"`
}

func uriFor(path string) string { return "file://" + path }

func runLSP(binary, root string, files []string) error {
	overall := time.Now()
	c, err := start(binary, root, "--lsp", "-stdio")
	if err != nil {
		return err
	}
	defer c.close()

	step := time.Now()
	if _, err := c.call("initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   uriFor(root),
		"capabilities": map[string]any{
			"workspace": map[string]any{
				"configuration":          false,
				"didChangeConfiguration": map[string]any{"dynamicRegistration": false},
				"didChangeWatchedFiles":  map[string]any{"dynamicRegistration": false},
			},
			"textDocument": map[string]any{
				"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"callHierarchy":  map[string]any{"dynamicRegistration": false},
			},
		},
	}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	fmt.Printf("lsp initialize        %v\n", time.Since(step).Round(time.Millisecond))

	// documentSymbol over every file: the node list.
	step = time.Now()
	type method struct {
		uri  string
		line int
		char int
	}
	var methods []method
	symbols := 0
	for _, path := range files {
		text, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		uri := uriFor(path)
		if err := c.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": "typescript", "version": 1, "text": string(text),
			},
		}); err != nil {
			return err
		}
		raw, err := c.call("textDocument/documentSymbol", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		})
		if err != nil {
			return fmt.Errorf("documentSymbol %s: %w", path, err)
		}
		var list []documentSymbol
		if err := json.Unmarshal(raw, &list); err != nil {
			continue
		}
		var walk func([]documentSymbol)
		walk = func(nodes []documentSymbol) {
			for _, node := range nodes {
				symbols++
				// 6 = Method, 12 = Function, 9 = Constructor in LSP's SymbolKind.
				if node.Kind == 6 || node.Kind == 12 {
					methods = append(methods, method{uri: uri, line: node.SelectionRange.Start.Line, char: node.SelectionRange.Start.Character})
				}
				walk(node.Children)
			}
		}
		walk(list)
	}
	fmt.Printf("documentSymbol×%-4d   %v  (%d symbols, %d callable)\n",
		len(files), time.Since(step).Round(time.Millisecond), symbols, len(methods))

	// Call hierarchy on a sample: two round trips per callable, and this is
	// where the checker actually runs.
	sample := methods
	step = time.Now()
	edges, prepared := 0, 0
	var examples []string
	for _, m := range sample {
		raw, err := c.call("textDocument/prepareCallHierarchy", map[string]any{
			"textDocument": map[string]any{"uri": m.uri},
			"position":     map[string]any{"line": m.line, "character": m.char},
		})
		if err != nil {
			return fmt.Errorf("prepareCallHierarchy: %w", err)
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
			continue
		}
		prepared++
		raw, err = c.call("callHierarchy/outgoingCalls", map[string]any{"item": items[0]})
		if err != nil {
			return fmt.Errorf("outgoingCalls: %w", err)
		}
		var calls []struct {
			To struct {
				Name   string `json:"name"`
				URI    string `json:"uri"`
				Detail string `json:"detail"`
			} `json:"to"`
			FromRanges []lspRange `json:"fromRanges"`
		}
		if err := json.Unmarshal(raw, &calls); err == nil {
			edges += len(calls)
			for _, call := range calls {
				if len(examples) < 6 && strings.Contains(call.To.URI, "/src/res0/") {
					examples = append(examples, fmt.Sprintf("%s:%d → %s  (%s)",
						filepath.Base(strings.TrimPrefix(m.uri, "file://")), m.line+1,
						call.To.Name, filepath.Base(strings.TrimPrefix(call.To.URI, "file://"))))
				}
			}
		}
	}
	elapsed := time.Since(step)
	per := time.Duration(0)
	if len(sample) > 0 {
		per = elapsed / time.Duration(len(sample))
	}
	fmt.Printf("callHierarchy×%-4d    %v  (%d prepared, %d outgoing edges, %v per callable)\n",
		len(sample), elapsed.Round(time.Millisecond), prepared, edges, per.Round(time.Microsecond))
	// The watcher's question: a file was saved, what does re-indexing cost on a
	// process that is already warm? This is what decides whether the TS
	// analyzer can be a one-shot subprocess like Java's and Lua's, or has to be
	// a long-lived one.
	if len(files) > 0 {
		step = time.Now()
		text, _ := os.ReadFile(files[0])
		uri := uriFor(files[0])
		_ = c.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": string(text) + "\nexport const touched = 1;\n"}},
		})
		if _, err := c.call("textDocument/documentSymbol", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		}); err != nil {
			return fmt.Errorf("warm documentSymbol: %w", err)
		}
		fmt.Printf("warm re-index of 1 file %v\n", time.Since(step).Round(time.Microsecond))
	}

	fmt.Printf("LSP TOTAL             %v\n", time.Since(overall).Round(time.Millisecond))
	fmt.Println("sample edges:")
	for _, example := range examples {
		fmt.Println("  " + example)
	}
	return nil
}

func collectTS(root string) []string {
	var files []string
	_ = filepath.Walk(filepath.Join(root, "src"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".ts") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files
}
