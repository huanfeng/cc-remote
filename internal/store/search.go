package store

import (
	"context"
	"database/sql"
	"math"
)

type MessageSearchScope struct {
	SessionID string
	Provider  string
	Cwd       string
}

type SearchMessage struct {
	Seq       int64
	SessionID string
	Kind      string
	Text      string
	CreatedAt string
	Title     string
	Cwd       string
	Provider  string
}

// VisitMessages releases the database between batches so a large search does
// not monopolize the connection used to persist live output.
func (s *Store) VisitMessages(ctx context.Context, scope MessageSearchScope, visit func(SearchMessage)) error {
	query := `SELECT e.seq,e.session_id,e.kind,e.text,e.created_at,s.title,s.cwd,s.provider
		FROM events e JOIN sessions s ON s.id=e.session_id
		WHERE e.kind IN ('user','assistant') AND e.text<>'' AND e.seq<?`
	var filters []any
	for _, filter := range []struct{ clause, value string }{
		{"e.session_id", scope.SessionID}, {"s.provider", scope.Provider}, {"s.cwd", scope.Cwd},
	} {
		if filter.value != "" {
			query += " AND " + filter.clause + "=?"
			filters = append(filters, filter.value)
		}
	}
	const batchSize = 128
	query += " ORDER BY e.seq DESC LIMIT ?"
	args := append(append([]any{int64(math.MaxInt64)}, filters...), batchSize)
	for {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		batch, err := scanSearchMessages(rows)
		if err != nil {
			return err
		}
		for _, message := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			visit(message)
		}
		if len(batch) < batchSize {
			return nil
		}
		args[0] = batch[len(batch)-1].Seq
	}
}

func scanSearchMessages(rows *sql.Rows) ([]SearchMessage, error) {
	defer rows.Close()
	var batch []SearchMessage
	for rows.Next() {
		var e SearchMessage
		if err := rows.Scan(&e.Seq, &e.SessionID, &e.Kind, &e.Text, &e.CreatedAt, &e.Title, &e.Cwd, &e.Provider); err != nil {
			return nil, err
		}
		batch = append(batch, e)
	}
	return batch, rows.Err()
}

func (s *Store) MessageContext(ctx context.Context, id string, seq int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT * FROM (
			SELECT seq,session_id,kind,text,'null' AS payload,created_at FROM events
			WHERE session_id=? AND seq<=? AND kind IN ('user','assistant') AND text<>''
			ORDER BY seq DESC LIMIT 11
		)
		UNION ALL
		SELECT * FROM (
			SELECT seq,session_id,kind,text,'null' AS payload,created_at FROM events
			WHERE session_id=? AND seq>? AND kind IN ('user','assistant') AND text<>''
			ORDER BY seq LIMIT 10
		) ORDER BY seq`, id, seq, id, seq)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}
