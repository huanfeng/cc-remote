package store

import (
	"database/sql"
	"fmt"
	"time"
)

// SessionStats is derived from stored events. Token and cost figures come from
// Claude's stream-json result events, so shared-terminal sessions without
// result events report CostKnown=false.
type SessionStats struct {
	SessionID           string  `json:"sessionId"`
	Turns               int     `json:"turns"`
	Results             int     `json:"results"`
	CostUSD             float64 `json:"costUSD"`
	CostKnown           bool    `json:"costKnown"`
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	DurationMs          int64   `json:"durationMs"`
	ToolCalls           int     `json:"toolCalls"`
	Errors              int     `json:"errors"`
	LastActivityAt      string  `json:"lastActivityAt"`
}

type DailyStats struct {
	Date    string  `json:"date"`
	Turns   int     `json:"turns"`
	CostUSD float64 `json:"costUSD"`
	Tokens  int64   `json:"tokens"`
}

// resultPayload yields the payload of a billable result event, else NULL. CASE keeps
// json_extract away from rows with invalid JSON. Error events only count when they
// are an is_error result; a permission-denial error repeats the preceding result.
const resultPayload = `CASE WHEN kind IN ('result','error') AND json_valid(payload) THEN
	CASE WHEN json_extract(payload,'$.type')='result' AND (kind='result' OR json_extract(payload,'$.is_error')=1) THEN payload END END`

const statsQuery = `SELECT session_id,SUM(kind='user'),COUNT(r),TOTAL(json_extract(r,'$.total_cost_usd')),
TOTAL(json_extract(r,'$.usage.input_tokens')),TOTAL(json_extract(r,'$.usage.output_tokens')),
TOTAL(json_extract(r,'$.usage.cache_read_input_tokens')),TOTAL(json_extract(r,'$.usage.cache_creation_input_tokens')),
TOTAL(json_extract(r,'$.duration_ms')),SUM(kind='tool'),SUM(kind='error'),MAX(created_at)
FROM (SELECT session_id,kind,created_at,` + resultPayload + ` AS r FROM events %s) GROUP BY session_id`

func (s *Store) queryStats(where string, args ...any) ([]SessionStats, error) {
	r, err := s.db.Query(fmt.Sprintf(statsQuery, where), args...)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := []SessionStats{}
	for r.Next() {
		var x SessionStats
		var in, outTok, cacheRead, cacheCreate, duration float64
		if err := r.Scan(&x.SessionID, &x.Turns, &x.Results, &x.CostUSD, &in, &outTok, &cacheRead, &cacheCreate, &duration, &x.ToolCalls, &x.Errors, &x.LastActivityAt); err != nil {
			return nil, err
		}
		x.CostKnown = x.Results > 0
		x.InputTokens, x.OutputTokens, x.CacheReadTokens, x.CacheCreationTokens, x.DurationMs = int64(in), int64(outTok), int64(cacheRead), int64(cacheCreate), int64(duration)
		out = append(out, x)
	}
	return out, r.Err()
}

// AllSessionStats returns statistics for every session that has events.
func (s *Store) AllSessionStats() ([]SessionStats, error) { return s.queryStats("") }

// SessionStatsFor returns zero statistics for a session without events.
func (s *Store) SessionStatsFor(id string) (SessionStats, error) {
	x, err := s.queryStats("WHERE session_id=?", id)
	if err != nil || len(x) == 0 {
		return SessionStats{SessionID: id}, err
	}
	return x[0], nil
}

// DailyStats returns one entry per UTC day, oldest first, including empty days.
func (s *Store) DailyStats(days int, now time.Time) ([]DailyStats, error) {
	start := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, 1-days)
	r, err := s.db.Query(`SELECT substr(created_at,1,10),SUM(kind='user'),TOTAL(json_extract(r,'$.total_cost_usd')),
TOTAL(json_extract(r,'$.usage.input_tokens'))+TOTAL(json_extract(r,'$.usage.output_tokens'))
FROM (SELECT kind,created_at,`+resultPayload+` AS r FROM events WHERE created_at>=?) GROUP BY 1`, start.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	byDay := map[string]DailyStats{}
	for r.Next() {
		var x DailyStats
		var tokens float64
		var turns sql.NullInt64
		if err := r.Scan(&x.Date, &turns, &x.CostUSD, &tokens); err != nil {
			return nil, err
		}
		x.Turns, x.Tokens = int(turns.Int64), int64(tokens)
		byDay[x.Date] = x
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	out := make([]DailyStats, 0, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		x := byDay[day]
		x.Date = day
		out = append(out, x)
	}
	return out, nil
}
