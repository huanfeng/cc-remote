package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"cc-remote/internal/store"
)

func TestStopDeniesBackgroundApprovalBetweenTurns(t *testing.T) {
	s, ts, st, procs := withFakeProc(t)
	x := createSession(t, ts)
	request(t, ts, "POST", "/api/sessions/"+x.ID+"/messages", map[string]string{"text": "start agent"}, testToken)
	p := <-procs
	await(t, func() bool { return len(p.sentCopy()) == 1 })
	p.replay("start agent")
	p.result(nil)
	s.mu.Lock()
	token := s.procs[x.ID].token
	s.mu.Unlock()
	got := make(chan answer, 1)
	go func() {
		req, _ := http.NewRequest("POST", ts.URL+"/api/bridge/request", strings.NewReader(`{"kind":"approval","toolName":"Write","input":{}}`))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			got <- answer{}
			return
		}
		defer resp.Body.Close()
		var a answer
		_ = json.NewDecoder(resp.Body).Decode(&a)
		got <- a
	}()
	var pending []store.Interaction
	await(t, func() bool { pending, _ = st.Pending(x.ID); return len(pending) == 1 })
	if code, b := request(t, ts, "POST", "/api/sessions/"+x.ID+"/stop", nil, testToken); code != 200 {
		t.Fatalf("stop %d %s", code, b)
	}
	select {
	case a := <-got:
		if a.Decision != "deny" {
			t.Fatalf("stop did not deny: %+v", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stop left background approval blocked")
	}
	await(t, func() bool { y, _ := st.Get(x.ID); return y.Status == "interrupted" })
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		t.Fatal("stopping an approval should not immediately kill the process")
	}
}
