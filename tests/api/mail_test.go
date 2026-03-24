//go:build integration

package apitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ---- MCP helpers ----

// mcpSession initialises an MCP session and returns the session ID.
func mcpSession(t *testing.T) string {
	t.Helper()
	url := fmt.Sprintf("%s/mcp", baseURL())

	initBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "1"},
		},
	}
	b, _ := json.Marshal(initBody)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("mcpSession: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mcpSession: request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("mcpSession: expected 200, got %d: %s", resp.StatusCode, body)
	}

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("mcpSession: no Mcp-Session-Id header in response")
	}
	return sessionID
}

// mcpCallTool calls an MCP tool by name and returns the text content of the result.
func mcpCallTool(t *testing.T, sessionID, toolName string, args map[string]any) string {
	t.Helper()
	url := fmt.Sprintf("%s/mcp", baseURL())

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("mcpCallTool %s: build request: %v", toolName, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mcpCallTool %s: request failed: %v", toolName, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("mcpCallTool %s: read body: %v", toolName, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mcpCallTool %s: expected 200, got %d: %s", toolName, resp.StatusCode, body)
	}

	var result struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("mcpCallTool %s: decode response: %v\nbody: %s", toolName, err, body)
	}
	if len(result.Result.Content) == 0 {
		return ""
	}
	return result.Result.Content[0].Text
}

// mailCount returns the total number of emails in Mailpit via the devctl proxy.
func mailCount(t *testing.T) int {
	t.Helper()
	body := httpGet(t, "/api/mail/api/v1/messages?limit=1")
	var resp struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("mailCount: decode response: %v\nbody: %s", err, body)
	}
	return resp.Total
}

// sendTestEmail injects a test email via Mailpit's SMTP port so we have something to delete.
func sendTestEmail(t *testing.T) {
	t.Helper()
	// Use Mailpit's test message API to inject a message without needing SMTP.
	url := fmt.Sprintf("%s/api/mail/api/v1/send", baseURL())
	msg := map[string]any{
		"From":    map[string]string{"Email": "test@example.com", "Name": "Test"},
		"To":      []map[string]string{{"Email": "dev@example.com", "Name": "Dev"}},
		"Subject": "deleteAllEmails test message",
		"Text":    "This message was sent by the deleteAllEmails integration test.",
	}
	b, _ := json.Marshal(msg)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("sendTestEmail: build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("sendTestEmail: request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("sendTestEmail: expected 200, got %d: %s", resp.StatusCode, body)
	}
}

// sendTestEmailAndGetID injects a test email and returns its message ID.
func sendTestEmailAndGetID(t *testing.T) string {
	t.Helper()
	sendTestEmail(t)

	// Fetch the most-recent message to get its ID.
	body := httpGet(t, "/api/mail/api/v1/messages?limit=1")
	var resp struct {
		Messages []struct {
			ID string `json:"ID"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("sendTestEmailAndGetID: decode response: %v\nbody: %s", err, body)
	}
	if len(resp.Messages) == 0 {
		t.Fatal("sendTestEmailAndGetID: no messages found after sending")
	}
	return resp.Messages[0].ID
}

// requireMailpit skips the test if Mailpit is not installed/running (proxy
// returns 502 or similar). Called at the top of every mail test so that runs
// without Mailpit present do not produce false failures.
func requireMailpit(t *testing.T) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/mail/api/v1/messages?limit=1", baseURL())) //nolint:noctx
	if err != nil {
		t.Skipf("requireMailpit: request error (Mailpit not available): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable {
		t.Skipf("requireMailpit: Mailpit not available (HTTP %d) — install it first", resp.StatusCode)
	}
}

// ---- Tests ----

// TestDeleteAllEmails_MCPTool_DeletesAllMessages verifies that calling the
// deleteAllEmails MCP tool removes all messages from Mailpit.
func TestDeleteAllEmails_MCPTool_DeletesAllMessages(t *testing.T) {
	requireMailpit(t)
	// Ensure there is at least one email to delete.
	sendTestEmail(t)

	beforeCount := mailCount(t)
	if beforeCount == 0 {
		t.Fatal("expected at least one email before deleteAllEmails, got 0")
	}

	session := mcpSession(t)
	result := mcpCallTool(t, session, "deleteAllEmails", map[string]any{})

	if !strings.Contains(result, "deleted") {
		t.Errorf("deleteAllEmails: unexpected result text: %q", result)
	}

	afterCount := mailCount(t)
	if afterCount != 0 {
		t.Errorf("deleteAllEmails: expected 0 emails after deletion, got %d", afterCount)
	}
}

// TestDeleteAllEmails_UIPath_DeletesAllMessages verifies that a bodyless DELETE
// to /api/mail/api/v1/messages removes all messages from Mailpit.
//
// This is the correct call pattern for the frontend's deleteAllMessages().
// The previous bug was that deleteAllMessages() sent {"IDs":["*"]} which
// Mailpit silently ignores, leaving all messages intact.
func TestDeleteAllEmails_UIPath_DeletesAllMessages(t *testing.T) {
	requireMailpit(t)
	// Ensure there is at least one email to delete.
	sendTestEmail(t)

	beforeCount := mailCount(t)
	if beforeCount == 0 {
		t.Fatal("expected at least one email before delete-all, got 0")
	}

	// Send the correct (fixed) request: a bodyless DELETE.
	_, status := httpDelete(t, "/api/mail/api/v1/messages")
	if status != 200 && status != 204 {
		t.Fatalf("DELETE /api/mail/api/v1/messages (no body): expected 200/204, got %d", status)
	}

	afterCount := mailCount(t)
	if afterCount != 0 {
		t.Errorf("deleteAllMessages: expected 0 emails after bodyless DELETE, got %d", afterCount)
	}
}

// TestDeleteEmails_MCPTool_DeletesSpecificMessages verifies that the
// deleteEmails MCP tool removes only the specified messages by ID.
func TestDeleteEmails_MCPTool_DeletesSpecificMessages(t *testing.T) {
	requireMailpit(t)
	// Send two emails and collect their IDs.
	id1 := sendTestEmailAndGetID(t)
	id2 := sendTestEmailAndGetID(t)

	beforeCount := mailCount(t)
	if beforeCount < 2 {
		t.Fatalf("expected at least 2 emails before deleteEmails, got %d", beforeCount)
	}

	session := mcpSession(t)
	result := mcpCallTool(t, session, "deleteEmails", map[string]any{
		"ids": id1 + "," + id2,
	})

	if !strings.Contains(result, "Deleted") {
		t.Errorf("deleteEmails: unexpected result text: %q", result)
	}

	afterCount := mailCount(t)
	expectedCount := beforeCount - 2
	if afterCount != expectedCount {
		t.Errorf("deleteEmails: expected %d emails after deletion of 2, got %d", expectedCount, afterCount)
	}
}

// TestDeleteEmails_UIPath_DeletesSpecificMessages verifies that DELETE
// /api/mail/api/v1/messages with a JSON body of specific IDs removes only
// those messages (the UI path for per-message deletion).
func TestDeleteEmails_UIPath_DeletesSpecificMessages(t *testing.T) {
	requireMailpit(t)
	// Send two emails and collect their IDs.
	id1 := sendTestEmailAndGetID(t)
	id2 := sendTestEmailAndGetID(t)

	beforeCount := mailCount(t)
	if beforeCount < 2 {
		t.Fatalf("expected at least 2 emails before delete, got %d", beforeCount)
	}

	// Simulate what the frontend deleteMessages(ids) call does.
	body, _ := json.Marshal(map[string]any{"IDs": []string{id1, id2}})
	url := fmt.Sprintf("%s/api/mail/api/v1/messages", baseURL())
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build DELETE request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		rb, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200/204, got %d: %s", resp.StatusCode, rb)
	}

	afterCount := mailCount(t)
	expectedCount := beforeCount - 2
	if afterCount != expectedCount {
		t.Errorf("delete UI path: expected %d emails after deletion of 2, got %d", expectedCount, afterCount)
	}
}
