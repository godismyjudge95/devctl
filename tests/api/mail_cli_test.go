//go:build integration

package apitest

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testMail struct {
	To, Subject, Text, HTML string
	Attachment              []byte
	AttachmentName          string
}

// sendMail injects a message through Mailpit's send API via the devctl proxy.
func sendMail(t *testing.T, m testMail) {
	t.Helper()
	body := map[string]any{
		"From":    map[string]string{"Email": "noreply@app.test", "Name": "App"},
		"To":      []map[string]string{{"Email": m.To}},
		"Subject": m.Subject,
		"Text":    m.Text,
		"HTML":    m.HTML,
	}
	if m.Attachment != nil {
		body["Attachments"] = []map[string]string{{
			"Content":  base64.StdEncoding.EncodeToString(m.Attachment),
			"Filename": m.AttachmentName,
		}}
	}
	if resp, status := httpPost(t, "/api/mail/api/v1/send", body); status != 200 {
		t.Fatalf("send mail: HTTP %d: %s", status, resp)
	}
}

// uniqueRecipient gives each test its own address so parallel runs and
// leftover mail from other tests cannot match.
func uniqueRecipient() string {
	return fmt.Sprintf("cli-%d@app.test", time.Now().UnixNano())
}

func mailCLI(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "devctl", args...)
	cmd.Env = append(os.Environ(), "DEVCTL_ADDR="+strings.TrimPrefix(baseURL(), "http://"))
	return cmd
}

func runDevctlCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := mailCLI(ctx, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String() + stderr.String(), err
	}
	return stdout.String(), nil
}

func mustDevctlCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runDevctlCLI(t, args...)
	if err != nil {
		t.Fatalf("devctl %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

type cliMail struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
	Text    string `json:"Text"`
}

func TestMailCLI_ListFiltersByRecipient(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	sendMail(t, testMail{To: to, Subject: "filter me", Text: "hello"})
	sendMail(t, testMail{To: "someone-else@app.test", Subject: "filter me", Text: "hello"})

	var resp struct {
		Messages []struct {
			To []struct{ Address string } `json:"To"`
		} `json:"messages"`
	}
	out := mustDevctlCLI(t, "mail:list", "--to="+to, "--since=5m", "--json")
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if len(resp.Messages) != 1 || resp.Messages[0].To[0].Address != to {
		t.Fatalf("mail:list --to=%s returned %+v, want exactly that recipient", to, resp.Messages)
	}
}

func TestMailCLI_LatestAndGetFormats(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	sendMail(t, testMail{To: to, Subject: "formats", Text: "plain-body-token", HTML: "<p>html-body-token</p>"})

	if out := mustDevctlCLI(t, "mail:latest", "--to="+to, "--format=text"); !strings.Contains(out, "plain-body-token") {
		t.Errorf("mail:latest --format=text = %q", out)
	}
	var msg cliMail
	out := mustDevctlCLI(t, "mail:latest", "--to="+to, "--json")
	if err := json.Unmarshal([]byte(out), &msg); err != nil || msg.ID == "" || msg.Subject != "formats" {
		t.Fatalf("mail:latest --json = %q (%v)", out, err)
	}
	if out := mustDevctlCLI(t, "mail:get", msg.ID, "--format=html"); !strings.Contains(out, "html-body-token") {
		t.Errorf("mail:get --format=html = %q", out)
	}
	if out := mustDevctlCLI(t, "mail:get", msg.ID, "--format=raw"); !strings.Contains(out, "Subject: formats") {
		t.Errorf("mail:get --format=raw missing the Subject header: %q", out)
	}
	if out, err := runDevctlCLI(t, "mail:get", msg.ID, "--format=pdf"); err == nil {
		t.Errorf("mail:get --format=pdf: want non-zero exit, got %q", out)
	}
}

func TestMailCLI_WaitPrintsNewMessage(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := mailCLI(ctx, "mail:wait", "--to="+to, "--timeout=20s", "--json")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	sendMail(t, testMail{To: to, Subject: "welcome aboard", Text: "verify"})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("mail:wait: %v\n%s", err, stderr.String())
	}
	var msg cliMail
	if err := json.Unmarshal(stdout.Bytes(), &msg); err != nil || msg.Subject != "welcome aboard" {
		t.Fatalf("mail:wait output = %q (%v)", stdout.String(), err)
	}
}

func TestMailCLI_WaitIgnoresExistingAndTimesOut(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	sendMail(t, testMail{To: to, Subject: "already here", Text: "old"})
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	out, err := runDevctlCLI(t, "mail:wait", "--to="+to, "--timeout=2s")
	if err == nil {
		t.Fatalf("mail:wait returned a message that existed before it started: %q", out)
	}
	if !strings.Contains(out, "within 2s") || time.Since(start) < 2*time.Second {
		t.Errorf("want a timeout error after 2s, got %q after %s", out, time.Since(start))
	}
}

func TestMailCLI_WaitSinceAcceptsAlreadyArrived(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	sendMail(t, testMail{To: to, Subject: "arrived first", Text: "x"})

	var msg cliMail
	out := mustDevctlCLI(t, "mail:wait", "--to="+to, "--since=1m", "--timeout=5s", "--json")
	if err := json.Unmarshal([]byte(out), &msg); err != nil || msg.Subject != "arrived first" {
		t.Fatalf("mail:wait --since output = %q (%v)", out, err)
	}
}

func TestMailCLI_Links(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	sendMail(t, testMail{
		To: to, Subject: "links",
		Text: "Open https://app.test/text-link now.",
		HTML: `<a href="https://app.test/verify?id=1&amp;sig=abc">Verify</a>`,
	})
	id := latestIDFor(t, to)

	var resp struct {
		Links []string `json:"links"`
	}
	out := mustDevctlCLI(t, "mail:links", id, "--json")
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	for _, want := range []string{"https://app.test/text-link", "https://app.test/verify?id=1&sig=abc"} {
		if !containsString(resp.Links, want) {
			t.Errorf("mail:links missing %q in %q", want, resp.Links)
		}
	}
}

func TestMailCLI_AttachmentsListAndSave(t *testing.T) {
	ensureInstalledRunning(t, "mailpit", 5*time.Minute)
	to := uniqueRecipient()
	content := []byte("invoice-bytes-123")
	sendMail(t, testMail{To: to, Subject: "invoice", Text: "see attached", Attachment: content, AttachmentName: "invoice.txt"})
	id := latestIDFor(t, to)

	var list struct {
		Attachments []struct {
			PartID   string `json:"PartID"`
			FileName string `json:"FileName"`
		} `json:"attachments"`
	}
	out := mustDevctlCLI(t, "mail:attachments", id, "--json")
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Attachments) != 1 || list.Attachments[0].FileName != "invoice.txt" {
		t.Fatalf("mail:attachments = %q (%v)", out, err)
	}

	dest := filepath.Join(t.TempDir(), "saved.txt")
	mustDevctlCLI(t, "mail:attachment", id, list.Attachments[0].PartID, "--out="+dest)
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("saved attachment = %q (%v), want %q", got, err, content)
	}
}

func latestIDFor(t *testing.T, to string) string {
	t.Helper()
	var msg cliMail
	out := mustDevctlCLI(t, "mail:latest", "--to="+to, "--json")
	if err := json.Unmarshal([]byte(out), &msg); err != nil || msg.ID == "" {
		t.Fatalf("mail:latest --to=%s: %q (%v)", to, out, err)
	}
	return msg.ID
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
