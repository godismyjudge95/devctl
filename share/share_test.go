package share

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Output lines copied from cloudflared 2026.10.x.
const (
	lineBanner = "2026-11-08T10:00:00Z INF Requesting new quick Tunnel on trycloudflare.com..."
	lineBox    = "2026-11-08T10:00:01Z INF |  https://rapid-orange-pine-delta.trycloudflare.com                                         |"
	lineDNS    = `2026-11-08T10:00:01Z ERR failed to request quick Tunnel: Post "https://api.trycloudflare.com/tunnel": dial tcp: lookup api.trycloudflare.com: no such host`
)

func TestParseURL(t *testing.T) {
	cases := map[string]string{
		lineBox:    "https://rapid-orange-pine-delta.trycloudflare.com",
		lineBanner: "",
		lineDNS:    "",
		"2026-11-08T10:00:01Z INF +--------------------------------------------------------------------------------------------+": "",
		"https://a-b.trycloudflare.com/": "https://a-b.trycloudflare.com",
	}
	for in, want := range cases {
		if got := ParseURL(in); got != want {
			t.Errorf("ParseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLineWriterSplitsChunks(t *testing.T) {
	var got []string
	w := &LineWriter{fn: func(l string) { got = append(got, l) }}
	fmt.Fprint(w, "one\r\ntw")
	fmt.Fprint(w, "o\n\nthree")
	if strings.Join(got, "|") != "one|two" {
		t.Fatalf("lines = %q", got)
	}
}

func TestCloudflaredArgsKeepSiteHost(t *testing.T) {
	got := strings.Join(CloudflaredArgs("shop.test", 443), " ")
	for _, want := range []string{
		"tunnel",
		"--url https://127.0.0.1:443",
		"--http-host-header shop.test",
		"--origin-server-name shop.test",
		"--no-tls-verify",
		"--no-autoupdate",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
}

type fakeProc struct {
	out     *LineWriter
	exit    chan error
	once    sync.Once
	stopped chan struct{}
}

func (p *fakeProc) Wait() error { return <-p.exit }

func (p *fakeProc) Stop() {
	p.once.Do(func() {
		close(p.stopped)
		p.exit <- nil
	})
}

func (p *fakeProc) emit(lines ...string) {
	for _, l := range lines {
		fmt.Fprintln(p.out, l)
	}
}

// fakeLauncher records every process it starts. script runs synchronously in
// the launcher, as if the process printed those lines immediately.
type fakeLauncher struct {
	mu     sync.Mutex
	procs  []*fakeProc
	script func(p *fakeProc)
	err    error
}

func (f *fakeLauncher) launch(domain string, out *LineWriter) (Process, error) {
	if f.err != nil {
		return nil, f.err
	}
	p := &fakeProc{out: out, exit: make(chan error, 1), stopped: make(chan struct{})}
	f.mu.Lock()
	f.procs = append(f.procs, p)
	f.mu.Unlock()
	if f.script != nil {
		f.script(p)
	}
	return p, nil
}

func (f *fakeLauncher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.procs)
}

func TestStartReturnsURL(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBanner, lineBox) }}
	m := NewManager(fl.launch)

	sh, err := m.Start(context.Background(), "shop-test", "shop.test")
	if err != nil {
		t.Fatal(err)
	}
	if sh.Status != Active || sh.URL != "https://rapid-orange-pine-delta.trycloudflare.com" {
		t.Fatalf("share = %+v", sh)
	}
	if sh.Domain != "shop.test" || sh.SiteID != "shop-test" {
		t.Fatalf("share = %+v", sh)
	}
	if list := m.List(); len(list) != 1 || list[0].URL != sh.URL {
		t.Fatalf("List = %+v", list)
	}
}

func TestStartIsIdempotentWhileLive(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBox) }}
	m := NewManager(fl.launch)
	ctx := context.Background()
	first, _ := m.Start(ctx, "shop-test", "shop.test")
	second, _ := m.Start(ctx, "shop-test", "shop.test")
	if fl.count() != 1 {
		t.Fatalf("launched %d processes, want 1", fl.count())
	}
	if first.URL != second.URL {
		t.Fatalf("URL changed: %q then %q", first.URL, second.URL)
	}
}

func TestStartTimesOutWhileStarting(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBanner) }}
	m := NewManager(fl.launch)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	sh, err := m.Start(ctx, "shop-test", "shop.test")
	if err != nil {
		t.Fatal(err)
	}
	if sh.Status != Starting || sh.URL != "" {
		t.Fatalf("share = %+v", sh)
	}

	fl.procs[0].emit(lineBox)
	if got := m.List()[0]; got.Status != Active || got.URL == "" {
		t.Fatalf("after URL line: %+v", got)
	}
}

func TestProcessExitBeforeURLFails(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) {
		p.emit(lineBanner, lineDNS)
		p.exit <- errors.New("exit status 1")
	}}
	m := NewManager(fl.launch)

	sh, err := m.Start(context.Background(), "shop-test", "shop.test")
	if err != nil {
		t.Fatal(err)
	}
	if sh.Status != Failed {
		t.Fatalf("status = %q, want failed", sh.Status)
	}
	if !strings.Contains(sh.Error, "no such host") {
		t.Fatalf("error = %q, want the cloudflared ERR line", sh.Error)
	}
	if list := m.List(); len(list) != 1 || list[0].Status != Failed {
		t.Fatalf("failed share must stay listed until stopped: %+v", list)
	}

	// Sharing again replaces the failed tunnel with a new process.
	fl.script = func(p *fakeProc) { p.emit(lineBox) }
	sh, _ = m.Start(context.Background(), "shop-test", "shop.test")
	if sh.Status != Active || fl.count() != 2 {
		t.Fatalf("retry: share = %+v, launches = %d", sh, fl.count())
	}
}

func TestExitAfterActiveMarksFailed(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBox) }}
	m := NewManager(fl.launch)
	m.Start(context.Background(), "shop-test", "shop.test")

	fl.procs[0].exit <- errors.New("signal: killed")
	deadline := time.Now().Add(time.Second)
	for m.List()[0].Status != Failed {
		if time.Now().After(deadline) {
			t.Fatal("share never became failed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := m.List()[0]
	if got.URL != "" || got.Error != "tunnel exited: signal: killed" {
		t.Fatalf("share = %+v", got)
	}
}

func TestStopEndsProcessAndRemovesShare(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBox) }}
	m := NewManager(fl.launch)
	m.Start(context.Background(), "shop-test", "shop.test")

	if !m.Stop("shop-test") {
		t.Fatal("Stop reported no share")
	}
	select {
	case <-fl.procs[0].stopped:
	default:
		t.Fatal("process was not stopped")
	}
	if len(m.List()) != 0 {
		t.Fatalf("List = %+v", m.List())
	}
	if m.Stop("shop-test") {
		t.Fatal("second Stop reported a share")
	}
}

func TestStopAll(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBox) }}
	m := NewManager(fl.launch)
	m.Start(context.Background(), "a-test", "a.test")
	m.Start(context.Background(), "b-test", "b.test")

	m.StopAll()
	if len(m.List()) != 0 {
		t.Fatalf("List = %+v", m.List())
	}
	for i, p := range fl.procs {
		select {
		case <-p.stopped:
		default:
			t.Fatalf("process %d still running", i)
		}
	}
}

func TestLaunchErrorLeavesNoShare(t *testing.T) {
	fl := &fakeLauncher{err: errors.New("no binary")}
	m := NewManager(fl.launch)
	if _, err := m.Start(context.Background(), "shop-test", "shop.test"); err == nil {
		t.Fatal("expected launch error")
	}
	if len(m.List()) != 0 {
		t.Fatalf("List = %+v", m.List())
	}
}

func TestListSortedByDomain(t *testing.T) {
	fl := &fakeLauncher{script: func(p *fakeProc) { p.emit(lineBox) }}
	m := NewManager(fl.launch)
	m.Start(context.Background(), "zed-test", "zed.test")
	m.Start(context.Background(), "api-test", "api.test")
	list := m.List()
	if list[0].Domain != "api.test" || list[1].Domain != "zed.test" {
		t.Fatalf("List = %+v", list)
	}
}
