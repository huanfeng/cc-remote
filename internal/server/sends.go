package server

import (
	"regexp"
	"time"
)

// A message sent on a poor network can arrive while its reply does not: the
// page then shows a failure, and sending again would give Claude the message
// twice. Each send carries an ID the page keeps until the host answers, so a
// resend with the same ID gets the first answer instead of a second message.

const (
	sendMemory   = time.Hour
	maxSendsKept = 512
)

// hostFeatures tells a page (GET /api/info) what this host takes beyond older
// ones: a request body with an unknown field is refused, so a page sends sendId
// only to a host that lists it.
var hostFeatures = []string{"sendId", "mentions", "codex", "codexSteer", "queueControls"}

var validSendID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type sentMessage struct {
	status string
	at     time.Time
}

// sends remembers the messages taken; the caller holds s.mu.
type sends map[string]sentMessage

func sendKey(session, id string) string { return session + "\x00" + id }

// seen is the status a message with this ID was taken with, if it was.
func (m sends) seen(session, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	sent, ok := m[sendKey(session, id)]
	if !ok || time.Since(sent.at) > sendMemory {
		return "", false
	}
	return sent.status, true
}

func (m sends) remember(session, id, status string) {
	if id == "" {
		return
	}
	now := time.Now()
	if len(m) >= maxSendsKept {
		var oldest string
		for k, v := range m {
			if now.Sub(v.at) > sendMemory {
				delete(m, k)
			} else if oldest == "" || v.at.Before(m[oldest].at) {
				oldest = k
			}
		}
		if len(m) >= maxSendsKept {
			delete(m, oldest)
		}
	}
	m[sendKey(session, id)] = sentMessage{status: status, at: now}
}
