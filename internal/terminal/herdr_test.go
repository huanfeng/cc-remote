package terminal

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type herdrCalls struct {
	mu    sync.Mutex
	calls [][]string
	fail  bool
}

func (h *herdrCalls) exec(_ context.Context, args []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, slices.Clone(args))
	if h.fail {
		return errors.New("herdr failed")
	}
	return nil
}

func (h *herdrCalls) all() [][]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.calls)
}

// argOf is the value after a flag in a call, or "" when the call has none.
func argOf(call []string, name string) string {
	for i := 0; i+1 < len(call); i++ {
		if call[i] == name {
			return call[i+1]
		}
	}
	return ""
}

func newTestBridge(h *herdrCalls) *herdrBridge {
	now := time.Unix(1_000, 0)
	return &herdrBridge{
		bin: "herdr", pane: "w1:p1", resume: []string{"ccr", "abc12345"},
		exec: h.exec,
		now:  func() time.Time { now = now.Add(time.Second); return now },
	}
}

func session(status string, online bool) *remoteSession {
	return &remoteSession{ID: "abc12345", Title: "demo", Status: status, Online: online}
}

func TestHerdrBridgeEnabledOnlyInsideHerdr(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "ccr" {
			return "/bin/ccr", nil
		}
		return "", errors.New("not found")
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	full := map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p1", "HERDR_BIN_PATH": "/usr/bin/herdr"}
	cases := []struct {
		name string
		env  map[string]string
		on   bool
	}{
		{"inside herdr", full, true},
		{"outside herdr", map[string]string{}, false},
		{"no pane", map[string]string{"HERDR_ENV": "1", "HERDR_BIN_PATH": "/usr/bin/herdr"}, false},
		{"not 1", map[string]string{"HERDR_ENV": "0", "HERDR_PANE_ID": "w1:p1", "HERDR_BIN_PATH": "/usr/bin/herdr"}, false},
		{"switched off", map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p1", "HERDR_BIN_PATH": "/usr/bin/herdr", "CC_REMOTE_HERDR": "0"}, false},
		{"no binary anywhere", map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p1"}, false},
	}
	for _, c := range cases {
		b := newHerdrBridge(env(c.env), look, defaultHost, "abc12345")
		if (b != nil) != c.on {
			t.Errorf("%s: bridge = %v, want enabled %v", c.name, b != nil, c.on)
		}
	}
	b := newHerdrBridge(env(full), look, defaultHost, "abc12345")
	if b.bin != "/usr/bin/herdr" || b.pane != "w1:p1" || !slices.Equal(b.resume, []string{"ccr", "abc12345"}) {
		t.Errorf("bridge = %+v", b)
	}
	if b := newHerdrBridge(env(full), func(string) (string, error) { return "", errors.New("no") }, defaultHost, "abc12345"); b == nil || b.resume != nil {
		t.Errorf("without ccr on PATH the bridge still reports but cannot offer a resume command: %+v", b)
	}
	// With no HERDR_BIN_PATH, herdr on PATH is used.
	b = newHerdrBridge(env(map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p1"}), func(name string) (string, error) { return "/x/" + name, nil }, defaultHost, "abc12345")
	if b == nil || b.bin != "/x/herdr" {
		t.Errorf("fallback to PATH: %+v", b)
	}
}

func TestHerdrStateMapping(t *testing.T) {
	for status, want := range map[string]string{"running": "working", "waiting": "blocked", "idle": "idle", "interrupted": "idle", "": "idle"} {
		if got := herdrState(status); got != want {
			t.Errorf("herdrState(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestHerdrBridgeReportsStateWithResumeAndSource(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	b.sync(context.Background(), session("running", true))
	calls := h.all()
	if len(calls) != 2 {
		t.Fatalf("first sync should report the agent then its display metadata, got %v", calls)
	}
	r := calls[0]
	if r[0] != "pane" || r[1] != "report-agent" || r[2] != "w1:p1" {
		t.Errorf("report call = %v", r)
	}
	if argOf(r, "--source") != "custom:cc-remote" || argOf(r, "--agent") != "claude" || argOf(r, "--state") != "working" {
		t.Errorf("report flags = %v", r)
	}
	if i := slices.Index(r, "--"); i < 0 || !slices.Equal(r[i+1:], []string{"ccr", "abc12345"}) {
		t.Errorf("resume command should follow --, got %v", r)
	}
	if argOf(r, "--message") != "" {
		t.Errorf("a working agent has no block message: %v", r)
	}
	m := calls[1]
	if m[1] != "report-metadata" || argOf(m, "--title") != "demo" || argOf(m, "--source") != "user:cc-remote" {
		t.Errorf("metadata call = %v", m)
	}
}

func TestHerdrBridgeBlockedCarriesMessage(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	b.sync(context.Background(), session("waiting", true))
	r := h.all()[0]
	if argOf(r, "--state") != "blocked" || argOf(r, "--message") == "" {
		t.Errorf("blocked report = %v", r)
	}
}

func TestHerdrBridgeOnlyReportsChanges(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	ctx := context.Background()
	b.sync(ctx, session("running", true))
	n := len(h.all())
	b.sync(ctx, session("running", true))
	b.sync(ctx, session("running", true))
	if len(h.all()) != n {
		t.Fatalf("unchanged state must not call herdr again: %v", h.all())
	}
	b.sync(ctx, session("waiting", true))
	b.sync(ctx, session("idle", true))
	calls := h.all()[n:]
	if len(calls) != 2 || argOf(calls[0], "--state") != "blocked" || argOf(calls[1], "--state") != "idle" {
		t.Fatalf("changes = %v", calls)
	}
	// The title is shown once, not on every change.
	for _, c := range calls {
		if c[1] == "report-metadata" {
			t.Errorf("metadata repeated: %v", c)
		}
	}
}

func TestHerdrBridgeSeqIncreases(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	ctx := context.Background()
	b.sync(ctx, session("running", true))
	b.sync(ctx, session("waiting", true))
	b.sync(ctx, nil) // session gone: release
	b.sync(ctx, session("idle", true))
	var seqs []int64
	for _, c := range h.all() {
		if s := argOf(c, "--seq"); s != "" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				t.Fatalf("seq %q: %v", s, err)
			}
			seqs = append(seqs, n)
		}
	}
	if len(seqs) != 4 {
		t.Fatalf("every report and release carries a seq, got %v", seqs)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("seq must increase strictly: %v", seqs)
		}
	}
}

func TestHerdrBridgeSeqIncreasesEvenWithAStuckClock(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	b.now = func() time.Time { return time.Unix(5, 0) }
	ctx := context.Background()
	b.sync(ctx, session("running", true))
	b.sync(ctx, session("waiting", true))
	var seqs []string
	for _, c := range h.all() {
		if s := argOf(c, "--seq"); s != "" {
			seqs = append(seqs, s)
		}
	}
	if len(seqs) != 2 || seqs[0] == seqs[1] {
		t.Fatalf("seqs = %v", seqs)
	}
}

func TestHerdrBridgeReleasesWhenOfflineAndReportsAgainOnReturn(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	ctx := context.Background()
	b.sync(ctx, session("running", true))
	b.sync(ctx, session("running", false))
	b.sync(ctx, session("running", false)) // still offline: nothing more to release
	b.sync(ctx, session("running", true))
	var verbs []string
	for _, c := range h.all() {
		verbs = append(verbs, c[1])
	}
	want := []string{"report-agent", "report-metadata", "release-agent", "report-agent", "report-metadata"}
	if !slices.Equal(verbs, want) {
		t.Fatalf("calls = %v, want %v", verbs, want)
	}
	rel := h.all()[2]
	if argOf(rel, "--source") != "custom:cc-remote" || argOf(rel, "--agent") != "claude" || rel[2] != "w1:p1" {
		t.Errorf("release call = %v", rel)
	}
}

func TestHerdrBridgeCloseReleasesOnceAndStops(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	ctx := context.Background()
	b.sync(ctx, session("waiting", true))
	b.close()
	b.close()
	b.sync(ctx, session("running", true)) // after close nothing is reported again
	n := 0
	for _, c := range h.all() {
		if c[1] == "release-agent" {
			n++
		}
	}
	if n != 1 || h.all()[len(h.all())-1][1] != "release-agent" {
		t.Fatalf("calls = %v", h.all())
	}
}

func TestHerdrBridgeCloseWithoutReportDoesNothing(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	b.close()
	if len(h.all()) != 0 {
		t.Fatalf("calls = %v", h.all())
	}
}

func TestHerdrBridgeFailureRetriesLaterWithoutSpinning(t *testing.T) {
	h := &herdrCalls{fail: true}
	b := newTestBridge(h)
	clock := time.Unix(1_000, 0)
	b.now = func() time.Time { return clock }
	ctx := context.Background()
	b.sync(ctx, session("running", true))
	if len(h.all()) != 1 {
		t.Fatalf("calls = %v", h.all())
	}
	clock = clock.Add(time.Second)
	b.sync(ctx, session("running", true)) // inside the back-off: no new process
	if len(h.all()) != 1 {
		t.Fatalf("a failing herdr must not be spawned every poll: %v", h.all())
	}
	h.mu.Lock()
	h.fail = false
	h.mu.Unlock()
	clock = clock.Add(time.Minute)
	b.sync(ctx, session("running", true))
	calls := h.all()
	if len(calls) < 2 || calls[1][1] != "report-agent" {
		t.Fatalf("the report should be retried after the back-off: %v", calls)
	}
	// A failed report must not be remembered as sent: release has nothing to undo.
	h2 := &herdrCalls{fail: true}
	b2 := newTestBridge(h2)
	b2.sync(ctx, session("running", true))
	b2.close()
	for _, c := range h2.all() {
		if c[1] == "release-agent" {
			t.Fatalf("released something that was never reported: %v", h2.all())
		}
	}
}

func TestHerdrBridgeRunFollowsTheSessionAndStops(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	var mu sync.Mutex
	status := "running"
	poll := func() (*remoteSession, error) {
		mu.Lock()
		defer mu.Unlock()
		return session(status, true), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.run(ctx, poll, 5*time.Millisecond); close(done) }()
	waitFor(t, func() bool { return hasState(h, "working") })
	mu.Lock()
	status = "waiting"
	mu.Unlock()
	waitFor(t, func() bool { return hasState(h, "blocked") })
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestHerdrBridgeRunIgnoresPollErrors(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	poll := func() (*remoteSession, error) { return nil, errors.New("host down") }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.run(ctx, poll, 5*time.Millisecond); close(done) }()
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done
	if len(h.all()) != 0 {
		t.Fatalf("an unreachable host says nothing about the session: %v", h.all())
	}
}

func hasState(h *herdrCalls, state string) bool {
	for _, c := range h.all() {
		if c[1] == "report-agent" && argOf(c, "--state") == state {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestHerdrArgsDoNotBreakOnOddTitles(t *testing.T) {
	h := &herdrCalls{}
	b := newTestBridge(h)
	x := session("idle", true)
	x.Title = "  --weird \"title\"\n with   control\x07 chars " + strings.Repeat("长", 100)
	b.sync(context.Background(), x)
	m := h.all()[1]
	title := argOf(m, "--title")
	if title == "" || strings.ContainsAny(title, "\n\x07") || len([]rune(title)) > 80 {
		t.Errorf("title = %q", title)
	}
}

func TestHerdrResumeKeepsANonDefaultHost(t *testing.T) {
	env := func(k string) string {
		return map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p1", "HERDR_BIN_PATH": "/h"}[k]
	}
	look := func(string) (string, error) { return "/bin/ccr", nil }
	b := newHerdrBridge(env, look, "https://box.example", "abc12345")
	if !slices.Equal(b.resume, []string{"ccr", "--host", "https://box.example", "abc12345"}) {
		t.Fatalf("resume = %v", b.resume)
	}
}

func TestHerdrTitleNeverReadsAsAFlag(t *testing.T) {
	for in, want := range map[string]string{"- -x": "x", "--a": "a", "  - b  c ": "b c", "-": "", "ok": "ok"} {
		if got := herdrText(in); got != want {
			t.Errorf("herdrText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHerdrMetadataFailureIsRetriedAndTitleChangesAreSent(t *testing.T) {
	h := &herdrCalls{}
	clock := time.Unix(1_000, 0)
	b := newTestBridge(h)
	b.now = func() time.Time { return clock }
	ctx := context.Background()
	failMeta := true
	b.exec = func(c context.Context, args []string) error {
		if args[1] == "report-metadata" && failMeta {
			_ = h.exec(c, args)
			return errors.New("no")
		}
		return h.exec(c, args)
	}
	b.sync(ctx, session("running", true)) // report ok, metadata fails
	failMeta = false
	clock = clock.Add(time.Minute)
	b.sync(ctx, session("running", true)) // metadata is retried
	x := session("running", true)
	x.Title = "renamed"
	b.sync(ctx, x) // a new title is sent
	var metas []string
	for _, c := range h.all() {
		if c[1] == "report-metadata" {
			metas = append(metas, argOf(c, "--title"))
		}
	}
	if !slices.Equal(metas, []string{"demo", "demo", "renamed"}) {
		t.Fatalf("metadata titles = %v (calls %v)", metas, h.all())
	}
	reports := 0
	for _, c := range h.all() {
		if c[1] == "report-agent" {
			reports++
		}
	}
	if reports != 1 {
		t.Fatalf("the state was reported %d times, want once", reports)
	}
}
