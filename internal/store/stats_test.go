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
	result := func(cost float64, isError bool) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": isError, "total_cost_usd": cost, "duration_ms": 1500, "num_turns": 3,
			"usage": map[string]int{"input_tokens": 100, "output_tokens": 20, "cache_read_input_tokens": 1000, "cache_creation_input_tokens": 50}})
		return b
	}
	for _, ev := range []struct {
		kind    string
		payload json.RawMessage
	}{
		{"user", nil}, {"tool", json.RawMessage(`{"type":"tool_use","name":"Read"}`)}, {"tool_result", nil}, {"result", result(0.25, false)},
		{"user", nil}, {"result", result(0.5, false)}, {"error", result(0.5, false)}, // permission denial repeats the result
		{"user", nil}, {"error", result(0.125, true)},
		{"error", nil},
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
	if m.Turns != 3 || m.Results != 3 || !m.CostKnown || math.Abs(m.CostUSD-0.875) > 1e-9 || m.InputTokens != 300 || m.OutputTokens != 60 ||
		m.CacheReadTokens != 3000 || m.CacheCreationTokens != 150 || m.DurationMs != 4500 || m.ToolCalls != 1 || m.Errors != 3 || m.LastActivityAt == "" {
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
	if len(daily) != 14 || today.Date != time.Now().UTC().Format("2006-01-02") || today.Turns != 4 || math.Abs(today.CostUSD-0.875) > 1e-9 || today.Tokens != 360 || daily[0].Turns != 0 {
		t.Fatalf("daily: %+v", daily)
	}
}
