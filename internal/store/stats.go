package store

import (
	"database/sql"
	"fmt"
	"time"
)

// SessionStats is derived from stored events. Token and cost figures come from
// Claude's stream-json result events, so terminal sessions without
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

// usageCTE defines ev (events filtered by %s, with their billable result payload r)
// and deltas (what each result adds to its session's spend). Claude reports
// total_cost_usd and modelUsage as running totals: continued by --resume, reset by
// /clear. So a result adds its increase over the previous one, a total that drops
// starts a new run, and zeroed totals (crash/startup-error results) are skipped.
const usageCTE = `ev AS (SELECT seq,session_id,kind,created_at,` + resultPayload + ` AS r FROM events %s),
res AS (SELECT * FROM (SELECT seq,session_id,created_at,json_extract(r,'$.total_cost_usd') AS cost,
	(SELECT TOTAL(json_extract(value,'$.inputTokens')) FROM json_each(r,'$.modelUsage')) AS inp,
	(SELECT TOTAL(json_extract(value,'$.outputTokens')) FROM json_each(r,'$.modelUsage')) AS outp,
	(SELECT TOTAL(json_extract(value,'$.cacheReadInputTokens')) FROM json_each(r,'$.modelUsage')) AS cr,
	(SELECT TOTAL(json_extract(value,'$.cacheCreationInputTokens')) FROM json_each(r,'$.modelUsage')) AS cc
	FROM ev WHERE r IS NOT NULL) WHERE cost>0),
prev AS (SELECT *,LAG(cost) OVER w AS pcost,LAG(inp) OVER w AS pinp,LAG(outp) OVER w AS poutp,LAG(cr) OVER w AS pcr,LAG(cc) OVER w AS pcc
	FROM res WINDOW w AS (PARTITION BY session_id ORDER BY seq)),
deltas AS (SELECT session_id,created_at,
	CASE WHEN pcost IS NULL OR cost<pcost THEN cost ELSE cost-pcost END AS cost,
	CASE WHEN pcost IS NULL OR cost<pcost THEN inp ELSE MAX(0,inp-pinp) END AS inp,
	CASE WHEN pcost IS NULL OR cost<pcost THEN outp ELSE MAX(0,outp-poutp) END AS outp,
	CASE WHEN pcost IS NULL OR cost<pcost THEN cr ELSE MAX(0,cr-pcr) END AS cr,
	CASE WHEN pcost IS NULL OR cost<pcost THEN cc ELSE MAX(0,cc-pcc) END AS cc FROM prev)`

const statsQuery = `WITH ` + usageCTE + `,
u AS (SELECT session_id,TOTAL(cost) AS cost,TOTAL(inp) AS inp,TOTAL(outp) AS outp,TOTAL(cr) AS cr,TOTAL(cc) AS cc FROM deltas GROUP BY session_id)
SELECT ev.session_id,SUM(kind='user'),COUNT(r),IFNULL(u.cost,0),IFNULL(u.inp,0),IFNULL(u.outp,0),IFNULL(u.cr,0),IFNULL(u.cc,0),
TOTAL(json_extract(r,'$.duration_ms')),SUM(kind='tool'),SUM(kind='error'),MAX(ev.created_at)
FROM ev LEFT JOIN u ON u.session_id=ev.session_id GROUP BY ev.session_id`

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
	// Deltas need each session's earlier results, so only the final rows are cut by date.
	since := start.Format(time.RFC3339)
	r, err := s.db.Query(`WITH `+fmt.Sprintf(usageCTE, "")+`
SELECT substr(created_at,1,10),SUM(turn),TOTAL(cost),TOTAL(tokens) FROM (
	SELECT created_at,kind='user' AS turn,0 AS cost,0 AS tokens FROM ev WHERE created_at>=?
	UNION ALL SELECT created_at,0,cost,inp+outp FROM deltas WHERE created_at>=?) GROUP BY 1`, since, since)
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
