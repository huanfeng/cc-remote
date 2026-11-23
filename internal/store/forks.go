package store

import "encoding/json"

// ForkReceipt records an accepted fork and the ordered upload IDs for its child.
type ForkReceipt struct {
	Source, RequestID, Fingerprint, SessionID string
	AttachmentIDs                             []string
}

func (s *Store) ForkReceipt(source, requestID string) (ForkReceipt, error) {
	x := ForkReceipt{Source: source, RequestID: requestID}
	var raw string
	err := s.db.QueryRow(`SELECT fingerprint,session_id,attachments FROM session_forks WHERE source=? AND request_id=?`, source, requestID).Scan(&x.Fingerprint, &x.SessionID, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &x.AttachmentIDs)
	}
	return x, err
}

// CreateFork publishes a child only after its complete local history is stored.
func (s *Store) CreateFork(x Session, history []Event, transcriptKeys []string, receipt ForkReceipt) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now()
	args, err := json.Marshal(x.ClaudeArgs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO sessions(id,title,cwd,claude_id,status,created_at,updated_at,resume,mode,permission_mode,model,effort,claude_args,provider,profile_id,profile_json,forked_from_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, x.ID, x.Title, x.Cwd, x.ClaudeSessionID, "idle", stamp, stamp, x.Resume, x.Mode, x.PermissionMode, x.Model, x.Effort, string(args), x.Provider, x.ProfileID, x.ProfileJSON, x.ForkedFromID)
	if err != nil {
		return err
	}
	for _, key := range transcriptKeys {
		if _, err = tx.Exec(`INSERT INTO channel_transcript(session_id,event_key) VALUES(?,?)`, x.ID, key); err != nil {
			return err
		}
	}
	for _, ev := range history {
		if !json.Valid(ev.Payload) {
			ev.Payload = json.RawMessage(`null`)
		}
		sourceAt := conversationTimestamp(ev.CreatedAt)
		storedAt := sourceAt
		if storedAt == "" {
			storedAt = stamp
		}
		if _, err = tx.Exec(`INSERT INTO events(session_id,kind,text,payload,created_at) VALUES(?,?,?,?,?)`, x.ID, ev.Kind, ev.Text, string(ev.Payload), storedAt); err != nil {
			return err
		}
		if err = recordConversation(tx, x.ID, ev.Kind, ev.Text, sourceAt); err != nil {
			return err
		}
	}
	if receipt.RequestID != "" {
		raw, err := json.Marshal(receipt.AttachmentIDs)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO session_forks(source,request_id,fingerprint,session_id,attachments) VALUES(?,?,?,?,?)`, receipt.Source, receipt.RequestID, receipt.Fingerprint, x.ID, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
