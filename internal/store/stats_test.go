package store

import (
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStats(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	managed, _ := st.Create("managed", t.TempDir(), ID(), false)
	term, _ := st.Create("terminal", t.TempDir(), ID(), true)
	empty, _ := st.Create("empty", t.TempDir(), ID(), false)
	// Results carry running totals; n scales one turn's usage, split across two models.
	result := func(cost float64, n int, isError bool) json.RawMessage {
		usage := func(k int) map[string]int {
			return map[string]int{"inputTokens": 50 * k, "outputTokens": 10 * k, "cacheReadInputTokens": 500 * k, "cacheCreationInputTokens": 25 * k}
		}
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": isError, "total_cost_usd": cost, "duration_ms": 1500, "num_turns": 3,
			"usage": map[string]int{"input_tokens": 100, "output_tokens": 20}, "modelUsage": map[string]any{"opus": usage(n), "haiku": usage(n)}})
		return b
	}
	for _, ev := range []struct {
		kind    string
		payload json.RawMessage
	}{
		{"user", nil}, {"tool", json.RawMessage(`{"type":"tool_use","name":"Read"}`)}, {"tool_result", nil}, {"result", result(0.25, 1, false)},
		{"user", nil}, {"result", result(0.5, 2, false)}, {"error", result(0.5, 2, false)}, // permission denial repeats the result
		{"user", nil}, {"error", result(0.625, 3, true)},
		{"error", nil},
		{"user", nil}, {"result", result(0, 0, true)}, // a crash result carries zeroed totals
		{"user", nil}, {"result", result(0.75, 4, false)}, // resumed: continues the saved total
		{"user", nil}, {"result", result(0.1, 1, false)}, // after /clear the total restarts
	} {
		if _, err := st.Append(managed.ID, ev.kind, "x", ev.payload); err != nil {
			t.Fatal(err)
		}
	}
	// Transcript rows are stored without validation; malformed JSON must not break aggregation.
	if _, err := st.RecordTranscript(term.ID, "k1", "user", "hi", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordTranscript(term.ID, "k2", "assistant", "hello", json.RawMessage(`{bad`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordTranscript(term.ID, "k3", "tool", "", json.RawMessage(`{"type":"tool_use"}`)); err != nil {
		t.Fatal(err)
	}
	all, err := st.AllSessionStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("stats rows: %+v", all)
	}
	m, err := st.SessionStatsFor(managed.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 0.75 through the resumed run, then 0.1 after /clear; tokens: 4 turns, then 1.
	if m.Turns != 6 || m.Results != 6 || !m.CostKnown || math.Abs(m.CostUSD-0.85) > 1e-9 || m.InputTokens != 500 || m.OutputTokens != 100 ||
		m.CacheReadTokens != 5000 || m.CacheCreationTokens != 250 || m.DurationMs != 9000 || m.ToolCalls != 1 || m.Errors != 3 || m.LastActivityAt == "" {
		t.Fatalf("managed stats: %+v", m)
	}
	c, _ := st.SessionStatsFor(term.ID)
	if c.Turns != 1 || c.ToolCalls != 1 || c.CostKnown || c.CostUSD != 0 || c.InputTokens != 0 {
		t.Fatalf("terminal stats: %+v", c)
	}
	e, err := st.SessionStatsFor(empty.ID)
	if err != nil || e.SessionID != empty.ID || e.Turns != 0 || e.LastActivityAt != "" {
		t.Fatalf("empty stats: %+v %v", e, err)
	}
	daily, err := st.DailyStats(14, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	today := daily[len(daily)-1]
	if len(daily) != 14 || today.Date != time.Now().UTC().Format("2006-01-02") || today.Turns != 7 || !today.CostKnown || math.Abs(today.CostUSD-0.85) > 1e-9 || today.Tokens != 600 || daily[0].Turns != 0 || daily[0].CostKnown {
		t.Fatalf("daily: %+v", daily)
	}
}

func TestCostAvailability(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project := t.TempDir()
	start := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name, kind, payload string
		results             int
		known               bool
		cost                float64
	}{
		{name: "no result", kind: "user"},
		{name: "missing amount", kind: "result", payload: `{"type":"result"}`, results: 1},
		{name: "null amount", kind: "result", payload: `{"type":"result","total_cost_usd":null}`, results: 1},
		{name: "text amount", kind: "result", payload: `{"type":"result","total_cost_usd":"1.25"}`, results: 1},
		{name: "negative amount", kind: "result", payload: `{"type":"result","total_cost_usd":-1}`, results: 1},
		{name: "zeroed error", kind: "error", payload: `{"type":"result","is_error":true,"total_cost_usd":0}`, results: 1},
		{name: "reported zero", kind: "result", payload: `{"type":"result","total_cost_usd":0}`, results: 1, known: true},
		{name: "reported spend", kind: "result", payload: `{"type":"result","total_cost_usd":1.25}`, results: 1, known: true, cost: 1.25},
	}
	for i, tc := range cases {
		x, err := st.Create(tc.name, project, ID(), false)
		if err != nil {
			t.Fatal(err)
		}
		at := start.AddDate(0, 0, i).Format(time.RFC3339)
		if _, err := st.AppendAt(x.ID, tc.kind, "", json.RawMessage(tc.payload), at); err != nil {
			t.Fatal(err)
		}
		stats, err := st.SessionStatsFor(x.ID)
		if err != nil || stats.Results != tc.results || stats.CostKnown != tc.known || stats.CostReported != tc.known || stats.CostUSD != tc.cost {
			t.Errorf("%s session: %+v, %v", tc.name, stats, err)
		}
	}
	daily, err := st.DailyStats(len(cases), start.AddDate(0, 0, len(cases)-1))
	if err != nil || len(daily) != len(cases) {
		t.Fatalf("daily: %+v, %v", daily, err)
	}
	for i, tc := range cases {
		if d := daily[i]; d.CostKnown != tc.known || d.CostUSD != tc.cost {
			t.Errorf("%s day: %+v", tc.name, d)
		}
	}
}
