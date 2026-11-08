package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeDaemon answers each path from routes (200 and the body, or "{}") and
// records every request as "METHOD /path?query".
func fakeDaemon(t *testing.T, routes map[string]string) (*Client, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		body, ok := routes[r.URL.Path]
		if !ok {
			body = "{}"
		}
		if strings.HasPrefix(body, "500 ") {
			http.Error(w, strings.TrimPrefix(body, "500 "), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{base: srv.URL, http: srv.Client()}, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// mcpSession feeds lines to the stdio server and returns the raw output and
// the decoded responses.
func mcpSession(t *testing.T, c *Client, lines ...string) (string, []map[string]any) {
	t.Helper()
	var out bytes.Buffer
	if err := serveMCP(c, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("serveMCP: %v", err)
	}
	raw := out.String()
	var resps []map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("decode response: %v\n%s", err, raw)
		}
		resps = append(resps, m)
	}
	return raw, resps
}

// mcpCall runs one tools/call and returns the text blocks and isError.
func mcpCall(t *testing.T, c *Client, tool string, args map[string]any) ([]string, bool) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
	_, resps := mcpSession(t, c, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":`+string(params)+`}`)
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d", len(resps))
	}
	if e := resps[0]["error"]; e != nil {
		t.Fatalf("tools/call %s: protocol error %v", tool, e)
	}
	res := resps[0]["result"].(map[string]any)
	var texts []string
	for _, c := range res["content"].([]any) {
		texts = append(texts, c.(map[string]any)["text"].(string))
	}
	isErr, _ := res["isError"].(bool)
	return texts, isErr
}

func TestMCP_InitializePingAndErrors(t *testing.T) {
	_, resps := mcpSession(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":4,"method":"resources/list"}`,
		`not json`,
	)
	if len(resps) != 5 {
		t.Fatalf("want 5 responses (none for the notification), got %d: %v", len(resps), resps)
	}
	for i, want := range []any{float64(1), float64(2), float64(3), float64(4), nil} {
		if resps[i]["id"] != want || resps[i]["jsonrpc"] != "2.0" {
			t.Errorf("response %d: id %v jsonrpc %v, want id %v", i, resps[i]["id"], resps[i]["jsonrpc"], want)
		}
	}

	initRes := resps[0]["result"].(map[string]any)
	if initRes["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's 2025-06-18", initRes["protocolVersion"])
	}
	if initRes["serverInfo"].(map[string]any)["name"] != "devctl" {
		t.Errorf("serverInfo = %v", initRes["serverInfo"])
	}
	if _, ok := initRes["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities has no tools: %v", initRes["capabilities"])
	}
	if v := resps[1]["result"].(map[string]any)["protocolVersion"]; v == "1999-01-01" || !slices.Contains([]any{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}, v) {
		t.Errorf("unsupported client version: got %v, want a version the server supports", v)
	}
	if r, ok := resps[2]["result"].(map[string]any); !ok || len(r) != 0 {
		t.Errorf("ping result = %v, want {}", resps[2]["result"])
	}
	if code := resps[3]["error"].(map[string]any)["code"]; code != float64(-32601) {
		t.Errorf("unknown method code = %v, want -32601", code)
	}
	if code := resps[4]["error"].(map[string]any)["code"]; code != float64(-32700) {
		t.Errorf("parse error code = %v, want -32700", code)
	}
}

// The owner's hard requirement: tools/list stays under 3k tokens.
func TestMCP_ToolsListFitsContextBudget(t *testing.T) {
	raw, resps := mcpSession(t, nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := resps[0]["result"].(map[string]any)["tools"].([]any)
	t.Logf("tools/list: %d tools, %d bytes, about %d tokens (bytes/4)", len(tools), len(raw), len(raw)/4)
	t.Logf("initialize instructions: %d bytes", len(mcpInstructions))
	if len(tools) < 8 || len(tools) > 12 {
		t.Errorf("want 8 to 12 tools, got %d", len(tools))
	}
	if len(raw) > 3000*4 {
		t.Errorf("tools/list is %d bytes (about %d tokens), want under 3000 tokens", len(raw), len(raw)/4)
	}
	for _, tl := range tools {
		tool := tl.(map[string]any)
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		enum := props["action"].(map[string]any)["enum"].([]any)
		if enum[0] != "help" {
			t.Errorf("%s: first action = %v, want help", tool["name"], enum[0])
		}
	}
}

func TestMCP_EveryExposedCommandHasExactlyOneTool(t *testing.T) {
	s := newMCPServer(nil)
	for _, cmd := range All() {
		var homes []string
		for _, tool := range s.tools {
			for action, c := range tool.actions {
				if c == cmd {
					homes = append(homes, tool.def.Name+"/"+action)
				}
			}
		}
		switch {
		case cmd.MCP == MCPHidden && len(homes) > 0:
			t.Errorf("%s is hidden but exposed as %v", cmd.Name, homes)
		case cmd.MCP != MCPHidden && len(homes) != 1:
			t.Errorf("%s: want exactly one MCP tool action, got %v. Add its namespace to mcpTools or set MCP: MCPHidden", cmd.Name, homes)
		}
	}
	for _, tool := range s.tools {
		if len(tool.order) != len(tool.actions) {
			t.Errorf("%s: two commands share an action name: %v", tool.def.Name, tool.order)
		}
	}
}

func TestMCP_CallPagesJSONList(t *testing.T) {
	var sites []string
	for i := range 120 {
		sites = append(sites, fmt.Sprintf(`{"id":"s%d","domain":"s%d.test"}`, i, i))
	}
	c, _ := fakeDaemon(t, map[string]string{"/api/sites": "[" + strings.Join(sites, ",") + "]"})

	decode := func(text string) []Site {
		var got []Site
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatalf("page is not a JSON site list: %v\n%s", err, text)
		}
		return got
	}

	texts, isErr := mcpCall(t, c, "sites", map[string]any{"action": "list"})
	if isErr || len(texts) != 2 {
		t.Fatalf("want a page and a note, got isError=%v %q", isErr, texts)
	}
	first := decode(texts[0])
	if len(first) == 0 || len(first) > 50 || first[0].Domain != "s0.test" {
		t.Fatalf("first page: %d sites, want 1-50 from s0.test", len(first))
	}
	if len(texts[0]) > mcpMaxBytes {
		t.Errorf("first page is %d bytes, over the %d byte cap", len(texts[0]), mcpMaxBytes)
	}
	if want := fmt.Sprintf("offset=%d", len(first)); !strings.Contains(texts[1], want) {
		t.Errorf("note %q does not say %s", texts[1], want)
	}

	texts, _ = mcpCall(t, c, "sites", map[string]any{"action": "list", "offset": 110, "limit": 10})
	if got := decode(texts[0]); len(got) != 10 || got[0].Domain != "s110.test" {
		t.Errorf("last page: %d sites starting %+v, want 10 from s110.test", len(got), got)
	}
	if len(texts) < 2 || strings.Contains(texts[1], "offset=") || !strings.Contains(texts[1], "of 120") {
		t.Errorf("last page note = %q", texts)
	}

	texts, _ = mcpCall(t, c, "sites", map[string]any{"action": "list", "limit": 500})
	if got := decode(texts[0]); len(got) >= 120 || len(texts) != 2 || len(texts[0]) > mcpMaxBytes {
		t.Errorf("limit=500: %d sites, %d bytes, %d blocks; want a valid page under the cap and a note", len(got), len(texts[0]), len(texts))
	}
}

func TestMCP_PageKeepsScalarListsWhole(t *testing.T) {
	var cols, rows []string
	for i := range 60 {
		cols = append(cols, fmt.Sprintf(`"c%d"`, i))
		rows = append(rows, fmt.Sprintf(`[%d]`, i))
	}
	text, note := pageOutput(`{"columns":[`+strings.Join(cols, ",")+`],"rows":[`+strings.Join(rows, ",")+`]}`, 0, 0)
	var got struct {
		Columns []string `json:"columns"`
		Rows    [][]int  `json:"rows"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Columns) != 60 || len(got.Rows) != 50 {
		t.Errorf("columns %d rows %d, want 60 columns and 50 rows", len(got.Columns), len(got.Rows))
	}
	if !strings.Contains(note, "rows 1-50 of 60") {
		t.Errorf("note = %q", note)
	}

	text, note = pageOutput("one\ntwo\nthree\n", 1, 1)
	if text != "two" || !strings.Contains(note, "offset=2") {
		t.Errorf("text paging: %q %q", text, note)
	}
}

func TestMCP_CallCapsLargeOutputAndBoundsLogTail(t *testing.T) {
	big := strings.Repeat("0123456789 log line\n", 2500)
	c, seen := fakeDaemon(t, map[string]string{"/api/logs/caddy/tail": big})

	texts, isErr := mcpCall(t, c, "logs", map[string]any{"action": "tail", "args": []string{"caddy"}})
	if isErr {
		t.Fatalf("tail failed: %q", texts)
	}
	if len(texts[0]) > mcpMaxBytes {
		t.Errorf("output is %d bytes, want at most %d", len(texts[0]), mcpMaxBytes)
	}
	if len(texts) < 2 || !strings.Contains(texts[1], "cut") {
		t.Errorf("no truncation note: %q", texts[1:])
	}
	if got := seen(); len(got) != 1 || !strings.Contains(got[0], "bytes=8192") {
		t.Errorf("requests = %v, want one tail with bytes=8192", got)
	}

	mcpCall(t, c, "logs", map[string]any{"action": "tail", "args": []string{"caddy", "--bytes=1000"}})
	if got := seen(); len(got) != 2 || !strings.Contains(got[1], "bytes=1000") {
		t.Errorf("requests = %v, want the caller's bytes=1000", got)
	}

	texts, isErr = mcpCall(t, c, "logs", map[string]any{"action": "tail", "args": []string{"caddy", "-f"}})
	if !isErr || len(seen()) != 2 {
		t.Errorf("--follow must be refused before any request: isError=%v %q", isErr, texts)
	}
}

func TestMCP_DestructiveActionNeedsConfirm(t *testing.T) {
	c, seen := fakeDaemon(t, nil)

	texts, isErr := mcpCall(t, c, "mail", map[string]any{"action": "clear"})
	if !isErr || !strings.Contains(texts[0], "confirm=true") {
		t.Errorf("clear without confirm: isError=%v %q", isErr, texts)
	}
	if got := seen(); len(got) != 0 {
		t.Fatalf("clear without confirm sent %v", got)
	}

	if texts, isErr := mcpCall(t, c, "mail", map[string]any{"action": "clear", "confirm": true}); isErr {
		t.Fatalf("clear with confirm failed: %q", texts)
	}
	if got := seen(); len(got) != 1 || got[0] != "DELETE /api/mail/api/v1/messages" {
		t.Errorf("requests = %v, want one DELETE of all messages", got)
	}
}

// db:drop asks on a terminal. Over MCP, confirm=true is the answer.
func TestMCP_ConfirmedDBDropDoesNotPrompt(t *testing.T) {
	c, seen := fakeDaemon(t, nil)
	texts, isErr := mcpCall(t, c, "db", map[string]any{"action": "drop", "args": []string{"mysql", "scratch"}, "confirm": true})
	if isErr {
		t.Fatalf("drop failed: %q", texts)
	}
	if got := seen(); !slices.Contains(got, "DELETE /api/databases/mysql/databases/scratch") {
		t.Errorf("requests = %v, want the drop", got)
	}
}

func TestMCP_HelpUnknownActionAndHandlerErrors(t *testing.T) {
	c, _ := fakeDaemon(t, map[string]string{"/api/sites": "500 boom"})

	texts, isErr := mcpCall(t, c, "sites", map[string]any{"action": "help"})
	if isErr || !strings.Contains(texts[0], "worktree:add") || !strings.Contains(texts[0], "--create") {
		t.Errorf("help: %q", texts)
	}
	texts, _ = mcpCall(t, c, "sites", map[string]any{"action": "help", "args": []string{"worktree:rm"}})
	if !strings.Contains(texts[0], "Needs confirm=true") || strings.Contains(texts[0], "worktree:add") {
		t.Errorf("filtered help: %q", texts)
	}

	texts, isErr = mcpCall(t, c, "sites", map[string]any{"action": "nope"})
	if !isErr || !strings.Contains(texts[0], "worktree:add") {
		t.Errorf("unknown action: isError=%v %q", isErr, texts)
	}

	texts, isErr = mcpCall(t, c, "sites", map[string]any{"action": "list"})
	if !isErr || !strings.Contains(strings.Join(texts, "\n"), "boom") {
		t.Errorf("daemon error: isError=%v %q", isErr, texts)
	}

	_, resps := mcpSession(t, c, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	if code := resps[0]["error"].(map[string]any)["code"]; code != float64(-32602) {
		t.Errorf("unknown tool code = %v, want -32602", code)
	}
}
