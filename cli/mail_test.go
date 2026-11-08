package cli

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMailFilter_Query(t *testing.T) {
	cases := []struct {
		f    mailFilter
		want string
	}{
		{mailFilter{}, ""},
		{mailFilter{To: "a@b.test"}, `to:"a@b.test"`},
		{mailFilter{To: "a@b.test", From: "noreply", Subject: `Say "hi" now`}, `to:"a@b.test" from:"noreply" subject:"Say hi now"`},
	}
	for _, tc := range cases {
		if got := tc.f.query(); got != tc.want {
			t.Errorf("query(%+v) = %q, want %q", tc.f, got, tc.want)
		}
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("15m", now)
	if err != nil || !got.Equal(now.Add(-15*time.Minute)) {
		t.Errorf("15m: got %v, %v", got, err)
	}
	got, err = parseSince("2026-11-06T10:00:00Z", now)
	if err != nil || !got.Equal(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("RFC 3339: got %v, %v", got, err)
	}
	if _, err := parseSince("yesterday", now); err == nil {
		t.Error("yesterday: want error")
	}
}

func TestParseMailArgs_FlagsInAnyPosition(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	out := fs.String("out", "", "")
	pos, err := parseMailArgs(fs, []string{"abc", "--out=x.pdf", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pos, []string{"abc", "2"}) || *out != "x.pdf" {
		t.Errorf("pos=%q out=%q", pos, *out)
	}
}

func TestExtractLinks_TextAndHTMLDeduplicated(t *testing.T) {
	text := "Verify: https://app.test/verify?token=abc&x=1.\nAgain (https://app.test/verify?token=abc&x=1)"
	htmlBody := `<a href="https://app.test/verify?token=abc&amp;x=1">Verify</a> <img src='http://cdn.test/logo.png'>`
	got := extractLinks(text, htmlBody)
	want := []string{"https://app.test/verify?token=abc&x=1", "http://cdn.test/logo.png"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractLinks = %q, want %q", got, want)
	}
	if got := extractLinks("", ""); got == nil || len(got) != 0 {
		t.Errorf("no links: want empty non-nil slice, got %#v", got)
	}
}

// fakeMailpit serves /api/mail/api/v1/{messages,search} newest first.
type fakeMailpit struct {
	mu       sync.Mutex
	messages []MailMessage
	queries  []string
}

func (f *fakeMailpit) add(m MailMessage) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append([]MailMessage{m}, f.messages...)
}

func (f *fakeMailpit) client(t *testing.T) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.queries = append(f.queries, r.URL.Path+"?"+r.URL.Query().Get("query"))
		var out []MailMessage
		q := r.URL.Query().Get("query")
		for _, m := range f.messages {
			if q == "" || strings.Contains(q, m.To[0].Address) {
				out = append(out, m)
			}
		}
		_ = json.NewEncoder(w).Encode(MailListResponse{Total: len(out), Messages: out})
	}))
	t.Cleanup(srv.Close)
	return NewClient(strings.TrimPrefix(srv.URL, "http://"))
}

func mailAt(id, to string, at time.Time) MailMessage {
	return MailMessage{ID: id, To: []MailAddress{{Address: to}}, Created: at.Format(time.RFC3339Nano)}
}

func TestWaitForMail_IgnoresOldAndFindsNewArrival(t *testing.T) {
	start := time.Now().Truncate(time.Millisecond)
	fake := &fakeMailpit{}
	fake.add(mailAt("old", "user@app.test", start.Add(-time.Second)))
	c := fake.client(t)

	go func() {
		time.Sleep(30 * time.Millisecond)
		fake.add(mailAt("other", "someone@app.test", start.Add(time.Millisecond)))
		fake.add(mailAt("new", "user@app.test", start.Add(2*time.Millisecond)))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := waitForMail(ctx, c, mailFilter{To: "user@app.test"}, start, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "new" {
		t.Errorf("got %q, want new", got.ID)
	}
	if !strings.Contains(fake.queries[0], `/api/mail/api/v1/search?to:"user@app.test"`) {
		t.Errorf("first request = %q, want a Mailpit search", fake.queries[0])
	}
}

// A message that lands after the command starts but before the first poll
// must count as new: "new" is decided by receive time, not by a baseline.
func TestWaitForMail_MessageBeforeFirstPollCounts(t *testing.T) {
	start := time.Now().Truncate(time.Millisecond)
	fake := &fakeMailpit{}
	fake.add(mailAt("raced", "user@app.test", start))
	c := fake.client(t)

	got, err := waitForMail(context.Background(), c, mailFilter{To: "user@app.test"}, start, time.Millisecond)
	if err != nil || got.ID != "raced" {
		t.Fatalf("got %q, %v; want raced", got.ID, err)
	}
}

func TestWaitForMail_ReturnsEarliestNewMessage(t *testing.T) {
	start := time.Now().Truncate(time.Millisecond)
	fake := &fakeMailpit{}
	fake.add(mailAt("first", "user@app.test", start.Add(time.Millisecond)))
	fake.add(mailAt("second", "user@app.test", start.Add(2*time.Millisecond)))
	c := fake.client(t)

	got, err := waitForMail(context.Background(), c, mailFilter{}, start, time.Millisecond)
	if err != nil || got.ID != "first" {
		t.Fatalf("got %q, %v; want first", got.ID, err)
	}
}

func TestWaitForMail_TimesOut(t *testing.T) {
	start := time.Now()
	fake := &fakeMailpit{}
	fake.add(mailAt("old", "user@app.test", start.Add(-time.Minute)))
	c := fake.client(t)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := waitForMail(ctx, c, mailFilter{}, start, 10*time.Millisecond)
	if err != context.DeadlineExceeded {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestResolveMailID_LatestUsesNewestMatch(t *testing.T) {
	now := time.Now()
	fake := &fakeMailpit{}
	fake.add(mailAt("a", "user@app.test", now))
	fake.add(mailAt("b", "other@app.test", now.Add(time.Second)))
	c := fake.client(t)

	id, err := resolveMailID(c, "latest", mailFilter{To: "user@app.test"})
	if err != nil || id != "a" {
		t.Fatalf("got %q, %v; want a", id, err)
	}
	if id, _ := resolveMailID(c, "xyz", mailFilter{}); id != "xyz" {
		t.Errorf("explicit id changed to %q", id)
	}
	if _, err := resolveMailID(c, "latest", mailFilter{To: "nobody@app.test"}); err == nil {
		t.Error("no match: want error")
	}
}
