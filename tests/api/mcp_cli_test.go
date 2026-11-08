//go:build integration

package apitest

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// mcpStdio runs one `devctl mcp` session: initialize, then reqs in order with
// ids 1..n. It returns each response line by id.
func mcpStdio(t *testing.T, reqs ...map[string]any) map[int]string {
	t.Helper()
	lines := []string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"apitest","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	}
	for i, r := range reqs {
		r["jsonrpc"], r["id"] = "2.0", i+1
		b, _ := json.Marshal(r)
		lines = append(lines, string(b))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := mailCLI(ctx, "mcp")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("devctl mcp: %v\nstderr: %s", err, stderr.String())
	}
	byID := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var head struct {
			ID *int `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &head); err != nil || head.ID == nil {
			t.Fatalf("bad response line %q (%v)", line, err)
		}
		byID[*head.ID] = line
	}
	if len(byID) != len(reqs)+1 {
		t.Fatalf("got %d responses for %d requests:\n%s", len(byID), len(reqs)+1, stdout.String())
	}
	return byID
}

func mcpToolCall(tool string, args map[string]any) map[string]any {
	return map[string]any{"method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}}
}

// mcpResult returns the text blocks of a tools/call response and isError.
func mcpResult(t *testing.T, line string) ([]string, bool) {
	t.Helper()
	var resp struct {
		Result *struct {
			Content []struct{ Text string } `json:"content"`
			IsError bool                    `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil || resp.Result == nil {
		t.Fatalf("not a tool result: %s (%v)", line, err)
	}
	var texts []string
	for _, c := range resp.Result.Content {
		texts = append(texts, c.Text)
	}
	return texts, resp.Result.IsError
}

func TestMCPStdio_ToolsListIsSmallAndCallsReachTheDaemon(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	sendMail(t, testMail{To: to, Subject: "mcp-roundtrip", Text: "hello from mcp"})

	res := mcpStdio(t,
		map[string]any{"method": "tools/list"},
		mcpToolCall("services", map[string]any{"action": "list"}),
		mcpToolCall("mail", map[string]any{"action": "latest", "args": []string{"--to=" + to}}),
	)

	if !strings.Contains(res[0], `"serverInfo":{"name":"devctl"`) {
		t.Errorf("initialize = %s", res[0])
	}

	list := res[1]
	t.Logf("tools/list: %d bytes, about %d tokens", len(list), len(list)/4)
	if len(list) > 3000*4 {
		t.Errorf("tools/list is %d bytes, want under 3000 tokens (12000 bytes)", len(list))
	}
	for _, name := range []string{"sites", "services", "php", "db", "mail", "logs", "dumps", "doctor", "settings"} {
		if !strings.Contains(list, `"name":"`+name+`"`) {
			t.Errorf("tools/list has no %s tool", name)
		}
	}

	if texts, isErr := mcpResult(t, res[2]); isErr || !strings.Contains(texts[0], `"mailpit"`) {
		t.Errorf("services list: isError=%v %q", isErr, texts)
	}

	texts, isErr := mcpResult(t, res[3])
	var msg struct {
		ID      string `json:"ID"`
		Subject string `json:"Subject"`
	}
	if isErr || json.Unmarshal([]byte(texts[0]), &msg) != nil || msg.Subject != "mcp-roundtrip" {
		t.Fatalf("mail latest: isError=%v %q", isErr, texts)
	}

	res = mcpStdio(t,
		mcpToolCall("mail", map[string]any{"action": "delete", "args": []string{msg.ID}}),
		mcpToolCall("mail", map[string]any{"action": "get", "args": []string{msg.ID}}),
		mcpToolCall("mail", map[string]any{"action": "delete", "args": []string{msg.ID}, "confirm": true}),
		mcpToolCall("mail", map[string]any{"action": "get", "args": []string{msg.ID}}),
	)
	if texts, isErr := mcpResult(t, res[1]); !isErr || !strings.Contains(texts[0], "confirm=true") {
		t.Errorf("delete without confirm: isError=%v %q", isErr, texts)
	}
	if texts, isErr := mcpResult(t, res[2]); isErr {
		t.Errorf("message gone after an unconfirmed delete: %q", texts)
	}
	if texts, isErr := mcpResult(t, res[3]); isErr {
		t.Errorf("delete with confirm: %q", texts)
	}
	if _, isErr := mcpResult(t, res[4]); !isErr {
		t.Errorf("message still there after a confirmed delete")
	}
}
