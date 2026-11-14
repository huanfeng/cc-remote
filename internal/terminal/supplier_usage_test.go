package terminal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadProfileUsageWireAndAuthentication(t *testing.T) {
	reset := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/profiles/usage" || r.URL.Query().Get("app") != "claude" || r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Errorf("unexpected usage request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"profiles":{"oauth:work":{"available":true,"identityKnown":true,"fetchedAt":%q,"windows":[{"name":"5 小时","percent":24,"windowMinutes":300,"resetsAt":%q}]}}}`, time.Now().UTC().Format(time.RFC3339), reset.Format(time.RFC3339))
	}))
	defer ts.Close()
	c := &client{host: ts.URL, token: "fixture-token"}
	u := c.readProfileUsage(context.Background())["oauth:work"]
	if !u.Available || len(u.Windows) != 1 || u.Windows[0].ResetsAt == nil || !u.Windows[0].ResetsAt.Equal(reset) || u.Windows[0].Percent != 24 {
		t.Fatalf("lost cached quota/reset timestamp: %+v", u)
	}
}

func TestReadProfileUsageDegradesGracefully(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{{"old host", 404, "not found"}, {"bad cache", 200, "invalid JSON"}, {"missing reset", 200, `{"profiles":{"native":{"windows":[{"percent":0}]}}}`}} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer ts.Close()
			got := (&client{host: ts.URL}).readProfileUsage(context.Background())
			if tc.name == "missing reset" {
				if len(got["native"].Windows) != 1 || got["native"].Windows[0].ResetsAt != nil {
					t.Fatalf("optional reset rejected: %+v", got)
				}
			} else if got != nil {
				t.Fatalf("expected unavailable cache, got %+v", got)
			}
		})
	}
}

func TestReadProfileUsageCancellation(t *testing.T) {
	started := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer ts.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan map[string]profileUsageSummary, 1)
	go func() { done <- (&client{host: ts.URL}).readProfileUsage(ctx) }()
	<-started
	cancel()
	select {
	case got := <-done:
		if got != nil {
			t.Fatalf("cancelled usage request returned data: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("usage request did not stop when picker closed")
	}
}

func TestReadProfileUsageTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer ts.Close()
	start := time.Now()
	if got := (&client{host: ts.URL}).readProfileUsage(context.Background()); got != nil {
		t.Fatalf("timed out usage returned data: %+v", got)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("usage lookup blocked too long: %s", elapsed)
	}
}

func TestSupplierUsageNotRequestedForExplicitOrNoninteractive(t *testing.T) {
	for _, tc := range []struct {
		name, explicit string
		interactive    bool
	}{{"explicit", "work", true}, {"noninteractive", "", false}} {
		t.Run(tc.name, func(t *testing.T) {
			var usageCalls atomic.Int32
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/profiles":
					fmt.Fprint(w, `{"profiles":[{"id":"work","name":"Work","app":"claude","kind":"oauth"}],"defaultProfiles":{"claude":"work"}}`)
				case "/api/profiles/work/env":
					fmt.Fprint(w, `{"name":"Work","env":{}}`)
				case "/api/profiles/usage":
					usageCalls.Add(1)
					fmt.Fprint(w, `{"profiles":{}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer ts.Close()
			_, _, err := (&client{host: ts.URL}).supplierEnvironment(tc.explicit, tc.interactive)
			if err != nil || usageCalls.Load() != 0 {
				t.Fatalf("unexpected usage lookup: calls=%d err=%v", usageCalls.Load(), err)
			}
		})
	}
}
