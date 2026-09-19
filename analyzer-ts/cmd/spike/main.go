// Command spike measures what it costs to index a TypeScript project through
// tsgo's API, and what comes back. Throwaway: it exists to answer whether the
// real analyzer should be built on this, and its numbers belong in a commit
// message rather than in a product.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type client struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	next int
}

// start spawns tsgo. The mode flag has to come first: tsgo switches on argv[1]
// before any flag parsing happens, and the two modes take different flags.
func start(binary, cwd string, mode string, args ...string) (*client, error) {
	cmd := exec.Command(binary, append([]string{mode}, args...)...)
	cmd.Dir = cwd
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &client{cmd: cmd, in: stdin, out: bufio.NewReaderSize(stdout, 1<<20), next: 1}, nil
}

// call sends one JSON-RPC request with LSP base-protocol framing and returns
// the raw result.
func (c *client) call(method string, params any) (json.RawMessage, error) {
	c.next++
	request := map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method}
	if params != nil {
		request["params"] = params
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		return nil, err
	}

	for {
		length := 0
		for {
			line, err := c.out.ReadString('\n')
			if err != nil {
				return nil, err
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				length, _ = strconv.Atoi(strings.TrimSpace(value))
			}
		}
		if length == 0 {
			continue
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.out, payload); err != nil {
			return nil, err
		}
		var envelope struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return nil, err
		}
		// A server-to-client request (registerCapability, workspace/configuration)
		// carries both an id and a method, and blocks the server until answered.
		// The API mode never does this; the LSP does, constantly.
		if envelope.Method != "" {
			if len(envelope.ID) > 0 {
				reply, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": json.RawMessage(envelope.ID), "result": []any{nil},
				})
				if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(reply), reply); err != nil {
					return nil, err
				}
			}
			continue
		}
		if string(envelope.ID) != strconv.Itoa(c.next) {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("%s: %s (code %d)", method, envelope.Error.Message, envelope.Error.Code)
		}
		return envelope.Result, nil
	}
}

func (c *client) close() {
	c.in.Close()
	_ = c.cmd.Wait()
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: spike <tsgo> <project>")
		os.Exit(2)
	}
	binary, root := os.Args[1], os.Args[2]
	if len(os.Args) > 3 && os.Args[3] == "lsp" {
		if err := runLSP(binary, root, collectTS(root)); err != nil {
			fmt.Fprintln(os.Stderr, "spike:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(binary, root); err != nil {
		fmt.Fprintln(os.Stderr, "spike:", err)
		os.Exit(1)
	}
}

func run(binary, root string) error {
	overall := time.Now()
	c, err := start(binary, root, "--api", "--cwd", root, "--async")
	if err != nil {
		return err
	}
	defer c.close()

	step := time.Now()
	if _, err := c.call("initialize", nil); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	fmt.Printf("initialize            %v\n", time.Since(step).Round(time.Millisecond))

	step = time.Now()
	raw, err := c.call("updateSnapshot", map[string]any{
		"openProjects": []any{filepath.Join(root, "tsconfig.json")},
	})
	if err != nil {
		return fmt.Errorf("updateSnapshot: %w", err)
	}
	// Handles are passed straight back, never through any: a SymbolID is a
	// 64-bit handle, and decoding it into an interface makes it a float64 —
	// which re-marshals with a rounded value the server refuses. This is the
	// single thing a real client must get right.
	var snapshot struct {
		Snapshot json.RawMessage `json:"snapshot"`
		Projects []struct {
			ID                json.RawMessage `json:"id"`
			ConfigFileName    string          `json:"configFileName"`
			ParsedCommandLine *struct {
				FileNames []string `json:"fileNames"`
			} `json:"parsedCommandLine"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	if len(snapshot.Projects) == 0 {
		return fmt.Errorf("no projects in snapshot: %s", raw)
	}
	project := snapshot.Projects[0]
	configured := 0
	if project.ParsedCommandLine != nil {
		configured = len(project.ParsedCommandLine.FileNames)
	}
	fmt.Printf("updateSnapshot        %v  (%d project(s), %d files in tsconfig)\n",
		time.Since(step).Round(time.Millisecond), len(snapshot.Projects), configured)

	step = time.Now()
	raw, err = c.call("getSourceFileNames", map[string]any{
		"snapshot": snapshot.Snapshot, "project": project.ID,
	})
	if err != nil {
		return fmt.Errorf("getSourceFileNames: %w", err)
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil {
		return fmt.Errorf("decode file names: %w", err)
	}
	project_files := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.Contains(name, "node_modules") && !strings.Contains(name, "/libs/lib.") {
			project_files = append(project_files, name)
		}
	}
	fmt.Printf("getSourceFileNames    %v  (%d total, %d of them the project's own)\n",
		time.Since(step).Round(time.Millisecond), len(names), len(project_files))

	// The batched call is the one that decides whether this is affordable: one
	// round trip for every file in the project rather than one per file.
	step = time.Now()
	files := make([]any, len(project_files))
	for i, name := range project_files {
		files[i] = name
	}
	raw, err = c.call("getSymbolsOfSourceFiles", map[string]any{
		"snapshot": snapshot.Snapshot, "project": project.ID, "files": files,
	})
	if err != nil {
		return fmt.Errorf("getSymbolsOfSourceFiles: %w", err)
	}
	var moduleSymbols []*struct {
		ID   json.RawMessage `json:"id"`
		Name string          `json:"name"`
	}
	if err := json.Unmarshal(raw, &moduleSymbols); err != nil {
		return fmt.Errorf("decode module symbols: %w", err)
	}
	resolved := 0
	for _, s := range moduleSymbols {
		if s != nil {
			resolved++
		}
	}
	fmt.Printf("getSymbolsOfSourceFiles %v  (%d module symbols, one batched call)\n",
		time.Since(step).Round(time.Millisecond), resolved)

	// Exports per module: this is the shape a real indexer would walk, and the
	// count of round trips it implies.
	step = time.Now()
	exports := 0
	calls := 0
	for _, s := range moduleSymbols {
		if s == nil {
			continue
		}
		raw, err := c.call("getExportsOfSymbol", map[string]any{
			"snapshot": snapshot.Snapshot, "project": project.ID, "objectId": s.ID,
		})
		calls++
		if err != nil {
			return fmt.Errorf("getExportsOfSymbol: %w", err)
		}
		var list []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &list); err == nil {
			exports += len(list)
		}
	}
	perCall := time.Duration(0)
	if calls > 0 {
		perCall = time.Since(step) / time.Duration(calls)
	}
	fmt.Printf("getExportsOfSymbol    %v  (%d calls, %d exports, %v per round trip)\n",
		time.Since(step).Round(time.Millisecond), calls, exports, perCall.Round(time.Microsecond))

	fmt.Printf("TOTAL                 %v\n", time.Since(overall).Round(time.Millisecond))
	return nil
}
