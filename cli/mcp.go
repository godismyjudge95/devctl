package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Version is the devctl version reported to MCP clients. main sets it.
var Version = "dev"

// mcpTool is one MCP tool. Its actions are the commands of its CLI
// namespaces, so a new CLI command shows up in MCP without extra code.
type mcpTool struct {
	name       string
	summary    string
	namespaces []string
}

var mcpTools = []mcpTool{
	{"sites", "Local *.test sites: list, details, PHP version, CORS, SPX, git branches and worktrees.", []string{"sites"}},
	{"services", "Dev services (Valkey, MySQL, PostgreSQL, Mailpit, …): status, start/stop, install, credentials. Also CLI helpers.", []string{"services", "helpers"}},
	{"php", "PHP versions, ini settings, SPX profiles.", []string{"php", "spx"}},
	{"db", "MySQL and PostgreSQL databases: list, create, drop, copy, import, export, query, site connection.", []string{"db", "postgres"}},
	{"mail", "Emails caught by Mailpit: list, read, wait for new, links, attachments, delete.", []string{"mail"}},
	{"logs", "Log files of services, PHP-FPM, and Caddy: list, tail, clear.", []string{"logs"}},
	{"dumps", "PHP dump() / php_dd() captures.", []string{"dumps"}},
	{"doctor", "Health checks of the devctl environment and safe fixes. DNS and elevate status.", []string{"doctor", "elevate", "dns"}},
	{"settings", "devctl settings.", []string{"settings"}},
}

const (
	mcpDefaultLimit = 50
	mcpMaxBytes     = 16 << 10
	mcpMaxCapture   = 4 << 20
)

// Newest first. initialize answers with the client's version when it is in
// this list, else with the newest.
var mcpProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

const mcpInstructions = "devctl runs the local PHP stack: *.test sites, PHP, databases, mail, logs, and services. " +
	"Each tool takes an action and args, the arguments of the CLI command `devctl <tool>:<action>`. " +
	"action=help shows usage. Output is JSON, paged with offset and limit. Destructive actions need confirm=true."

// mcpDefaultArgs keep a default call under the response cap.
var mcpDefaultArgs = map[string][]string{"logs:tail": {"--bytes=8192"}}

func init() {
	Register(&Cmd{
		Name:        "mcp",
		MCP:         MCPHidden,
		Description: "Run the MCP server on stdio for AI agents. Set it up with mcp:install",
		Examples:    []string{"devctl mcp", "DEVCTL_ADDR=127.0.0.1:4001 devctl mcp"},
		Handler: func(c *Client, args []string, jsonMode bool) error {
			return serveMCP(c, os.Stdin, os.Stdout)
		},
	})
}

type mcpToolIndex struct {
	def     mcpToolDef
	actions map[string]*Cmd
	order   []string
}

type mcpToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpServer struct {
	client *Client
	tools  []*mcpToolIndex
}

func newMCPServer(c *Client) *mcpServer {
	s := &mcpServer{client: c}
	for _, t := range mcpTools {
		s.tools = append(s.tools, indexMCPTool(t, All()))
	}
	return s
}

func indexMCPTool(t mcpTool, cmds []*Cmd) *mcpToolIndex {
	idx := &mcpToolIndex{actions: map[string]*Cmd{}}
	destructive := false
	for _, cmd := range cmds {
		if cmd.MCP == MCPHidden || !slices.Contains(t.namespaces, namespaceOf(cmd.Name)) {
			continue
		}
		action := strings.TrimPrefix(cmd.Name, t.name+":")
		idx.actions[action] = cmd
		idx.order = append(idx.order, action)
		destructive = destructive || cmd.MCP == MCPConfirm
	}
	props := map[string]any{
		"action": map[string]any{"type": "string", "enum": append([]string{"help"}, idx.order...)},
		"args":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "CLI arguments and --flags"},
		"offset": map[string]any{"type": "integer", "description": "Skip this many items or lines"},
		"limit":  map[string]any{"type": "integer", "description": fmt.Sprintf("Max items or lines (default %d)", mcpDefaultLimit)},
	}
	if destructive {
		props["confirm"] = map[string]any{"type": "boolean", "description": "Required for destructive actions"}
	}
	idx.def = mcpToolDef{
		Name:        t.name,
		Description: t.summary + " action=help for usage.",
		InputSchema: map[string]any{"type": "object", "properties": props, "required": []string{"action"}},
	}
	return idx
}

func namespaceOf(name string) string {
	ns, _, _ := strings.Cut(name, ":")
	return ns
}

func (s *mcpServer) tool(name string) *mcpToolIndex {
	for _, t := range s.tools {
		if t.def.Name == name {
			return t
		}
	}
	return nil
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// serveMCP reads newline-delimited JSON-RPC from in and answers on out until
// in closes. Requests run one at a time.
func serveMCP(c *Client, in io.Reader, out io.Writer) error {
	s := newMCPServer(c)
	r := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if resp := s.handle(line); resp != nil {
				if werr := enc.Encode(resp); werr != nil {
					return werr
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (s *mcpServer) handle(line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error: " + err.Error()}}
	}
	if len(req.ID) == 0 {
		return nil
	}
	result, rerr := s.dispatch(req.Method, req.Params)
	if rerr != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *mcpServer) dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		version := mcpProtocolVersions[0]
		if slices.Contains(mcpProtocolVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "devctl", "version": Version},
			"instructions":    mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		defs := make([]mcpToolDef, len(s.tools))
		for i, t := range s.tools {
			defs[i] = t.def
		}
		return map[string]any{"tools": defs}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{-32602, "invalid params: " + err.Error()}
		}
		t := s.tool(p.Name)
		if t == nil {
			return nil, &rpcError{-32602, "unknown tool: " + p.Name}
		}
		return s.call(t, p.Arguments), nil
	}
	return nil, &rpcError{-32601, "method not found: " + method}
}

type mcpCallArgs struct {
	Action  string   `json:"action"`
	Args    []string `json:"args"`
	Confirm bool     `json:"confirm"`
	Offset  int      `json:"offset"`
	Limit   int      `json:"limit"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

func textResult(isError bool, texts ...string) mcpResult {
	r := mcpResult{Content: []mcpContent{}, IsError: isError}
	for _, t := range texts {
		if t != "" {
			r.Content = append(r.Content, mcpContent{Type: "text", Text: t})
		}
	}
	return r
}

func (s *mcpServer) call(t *mcpToolIndex, raw json.RawMessage) mcpResult {
	var a mcpCallArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return textResult(true, "invalid arguments: "+err.Error())
		}
	}
	if a.Action == "help" {
		return textResult(false, t.help(a.Args))
	}
	cmd := t.actions[a.Action]
	if cmd == nil {
		return textResult(true, fmt.Sprintf("unknown action %q. Actions: help, %s", a.Action, strings.Join(t.order, ", ")))
	}
	if a.Offset < 0 || a.Limit < 0 {
		return textResult(true, "offset and limit must not be negative")
	}
	if cmd.MCP == MCPConfirm && !a.Confirm {
		return textResult(true, cmd.Name+" deletes or replaces data. Call again with confirm=true only if the user wants that.")
	}
	args, err := mcpCommandArgs(cmd, a.Args, a.Confirm)
	if err != nil {
		return textResult(true, err.Error())
	}
	out, overflow, herr := runCaptured(cmd, s.client, args)
	text, note := pageOutput(out, a.Offset, a.Limit)
	if capped, cut := capBytes(text, mcpMaxBytes); cut || overflow {
		text = capped
		note = strings.TrimSpace(note + fmt.Sprintf(" Output cut at %d KB. Use offset, limit, or a narrower filter.", mcpMaxBytes>>10))
	}
	if herr != nil {
		return textResult(true, text, note, "error: "+herr.Error())
	}
	if text == "" {
		text = "ok"
	}
	return textResult(false, text, note)
}

// mcpCommandArgs turns the tool args into the handler's CLI args.
func mcpCommandArgs(cmd *Cmd, in []string, confirm bool) ([]string, error) {
	_, _, args, err := splitGlobalFlags(in)
	if err != nil {
		return nil, err
	}
	has := func(flag string) bool {
		return slices.ContainsFunc(args, func(a string) bool { return a == flag || strings.HasPrefix(a, flag+"=") })
	}
	if has("--follow") || has("-f") {
		return nil, fmt.Errorf("--follow streams forever and is not available over MCP")
	}
	for _, def := range mcpDefaultArgs[cmd.Name] {
		name, _, _ := strings.Cut(def, "=")
		if !has(name) {
			args = append(args, def)
		}
	}
	if confirm && cmd.MCP == MCPConfirm && slices.ContainsFunc(cmd.Flags, func(f FlagDef) bool { return f.Name == "force" }) && !has("--force") {
		args = append(args, "--force")
	}
	return args, nil
}

var mcpRunMu sync.Mutex

// runCaptured runs a CLI handler in JSON mode. Handlers print to os.Stdout,
// which is the JSON-RPC channel here, so it points os.Stdout at a pipe for
// the call. os.Stdin points at /dev/null so no handler reads protocol bytes.
func runCaptured(cmd *Cmd, c *Client, args []string) (out string, overflow bool, err error) {
	mcpRunMu.Lock()
	defer mcpRunMu.Unlock()
	r, w, err := os.Pipe()
	if err != nil {
		return "", false, err
	}
	defer r.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		w.Close()
		return "", false, err
	}
	type captured struct {
		data     []byte
		overflow bool
	}
	done := make(chan captured, 1)
	go func() {
		data, _ := io.ReadAll(io.LimitReader(r, mcpMaxCapture+1))
		over := len(data) > mcpMaxCapture
		if over {
			data = data[:mcpMaxCapture]
			_, _ = io.Copy(io.Discard, r)
		}
		done <- captured{data, over}
	}()

	stdout, stdin := os.Stdout, os.Stdin
	os.Stdout, os.Stdin = w, null
	func() {
		defer func() {
			os.Stdout, os.Stdin = stdout, stdin
			if p := recover(); p != nil {
				err = fmt.Errorf("%s panicked: %v", cmd.Name, p)
			}
		}()
		err = cmd.Handler(c, args, true)
	}()
	w.Close()
	null.Close()
	res := <-done
	return string(res.data), res.overflow, err
}

// pageOutput returns one page of a command's output and a note when items
// were left out. JSON is paged by its list (the top-level array, or the
// largest array of records in an object) and compacted. Other text is paged
// by line.
func pageOutput(raw string, offset, limit int) (string, string) {
	if limit <= 0 {
		limit = mcpDefaultLimit
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err == nil && !dec.More() {
		// A page that is too large for the response cap gets fewer items, so
		// the client always receives valid JSON and an offset to continue.
		for {
			out, note, n := pageJSON(v, offset, limit)
			if len(out) <= mcpMaxBytes || n <= 1 {
				return out, note
			}
			limit = max(1, n*mcpMaxBytes/len(out))
			if limit >= n {
				limit = n - 1
			}
		}
	}
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return "", ""
	}
	lines, note := pageSlice(strings.Split(raw, "\n"), offset, limit, "lines")
	return strings.Join(lines, "\n"), note
}

// pageJSON pages the list in v and returns the compact JSON, the note, and
// the number of items on the page (0 when nothing was paged).
func pageJSON(v any, offset, limit int) (string, string, int) {
	note, n := "", 0
	switch x := v.(type) {
	case []any:
		page, nt := pageSlice(x, offset, limit, "items")
		v, note, n = page, nt, len(page)
	case map[string]any:
		if key := largestRecordList(x); key != "" {
			page, nt := pageSlice(x[key].([]any), offset, limit, key)
			cp := make(map[string]any, len(x))
			for k, val := range x {
				cp[k] = val
			}
			cp[key] = page
			v, note, n = cp, nt, len(page)
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(buf.String(), "\n"), note, n
}

func pageSlice[T any](items []T, offset, limit int, what string) ([]T, string) {
	total := len(items)
	offset = min(offset, total)
	end := min(offset+limit, total)
	if offset == 0 && end == total {
		return items, ""
	}
	if offset == end {
		return items[offset:end], fmt.Sprintf("No %s at offset %d. Total %d.", what, offset, total)
	}
	note := fmt.Sprintf("Showing %s %d-%d of %d.", what, offset+1, end, total)
	if end < total {
		note += fmt.Sprintf(" Call again with offset=%d for more.", end)
	}
	return items[offset:end], note
}

// largestRecordList names the longest array of objects or arrays in m. Lists
// of scalars (column names, tags) are never paged, so a page keeps them whole.
func largestRecordList(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, n := "", 0
	for _, k := range keys {
		arr, ok := m[k].([]any)
		if !ok || len(arr) <= n {
			continue
		}
		switch arr[0].(type) {
		case map[string]any, []any:
			best, n = k, len(arr)
		}
	}
	return best
}

func capBytes(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// help renders the registry docs for the tool's actions, or for the actions
// named in filter.
func (t *mcpToolIndex) help(filter []string) string {
	var sb strings.Builder
	sb.WriteString("Call with {action, args}. args are the CLI arguments and --flags. Output is JSON.\n")
	for _, action := range t.order {
		if len(filter) > 0 && !slices.Contains(filter, action) {
			continue
		}
		cmd := t.actions[action]
		fmt.Fprintf(&sb, "\n%s\n  %s", strings.TrimSpace(action+" "+cmd.Usage), cmd.Description)
		if cmd.MCP == MCPConfirm {
			if !strings.HasSuffix(cmd.Description, ".") {
				sb.WriteString(".")
			}
			sb.WriteString(" Needs confirm=true.")
		}
		sb.WriteString("\n")
		for _, a := range cmd.Args {
			opt := ""
			if a.Optional {
				opt = " (optional)"
			}
			fmt.Fprintf(&sb, "  <%s>%s %s\n", a.Name, opt, a.Description)
		}
		for _, f := range cmd.Flags {
			def := ""
			if f.Default != "" {
				def = " (default " + f.Default + ")"
			}
			fmt.Fprintf(&sb, "  --%s%s %s\n", f.Name, def, f.Description)
		}
		for _, e := range cmd.Examples[:min(2, len(cmd.Examples))] {
			fmt.Fprintf(&sb, "  e.g. %s\n", e)
		}
	}
	return sb.String()
}
