package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSubscriptionUsageWithoutInference(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "usage")
	u, err := ReadSubscriptionUsage(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Available || u.Plan != "plus" || u.Usage == nil || len(u.Usage.Windows) != 2 {
		t.Fatalf("usage: %+v", u)
	}
	a, b := u.Usage.Windows[0], u.Usage.Windows[1]
	if a.Percent != 23.5 || a.WindowMinutes != 300 || a.ResetsAt == "" || b.Percent != 0 || b.WindowMinutes != 10080 {
		t.Fatalf("windows: %+v", u.Usage.Windows)
	}
	wire, _ := json.Marshal(u)
	if strings.Contains(string(wire), "private@example.com") {
		t.Fatal("account identity leaked")
	}
}

func TestSubscriptionUsageAuthenticationKinds(t *testing.T) {
	for _, tc := range []struct{ account, provider, reason string }{
		{"none", "", "no-login"}, {"apiKey", "", "api-key"}, {"", "custom", "third-party"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			o := fakeOptions(t)
			t.Setenv("CODEX_ADAPTER_TEST_MODE", "usage")
			t.Setenv("CODEX_ADAPTER_TEST_ACCOUNT", tc.account)
			t.Setenv("CODEX_ADAPTER_TEST_PROVIDER", tc.provider)
			u, err := ReadSubscriptionUsage(context.Background(), o)
			if err != nil {
				t.Fatal(err)
			}
			if u.Available || u.Reason != tc.reason || u.Usage != nil {
				t.Fatalf("usage: %+v", u)
			}
		})
	}
}

func TestConfigureAppliesNextTurnAndRestoresInheritance(t *testing.T) {
	o := fakeOptions(t)
	t.Setenv("CODEX_ADAPTER_TEST_MODE", "configure")
	p, l := startFake(t, o)
	first := receive(t, l.events)
	if !strings.Contains(string(first.Payload), `"model":"model-a"`) || !strings.Contains(string(first.Payload), `"effort":"medium"`) {
		t.Fatalf("missing reported configuration: %s", first.Payload)
	}
	if err := p.Send("hold"); err != nil {
		t.Fatal(err)
	}
	receive(t, l.replays)
	p.Configure("model-b", "high")
	if err := p.Send("second"); err != nil {
		t.Fatal(err)
	}
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, l.results); err == nil {
		t.Fatal("first turn was not interrupted")
	}
	if got := receive(t, l.replays); got != "second" {
		t.Fatal(got)
	}
	if err := receive(t, l.results); err != nil {
		t.Fatal(err)
	}
	p.Configure("", "")
	if err := p.Send("inherited"); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, l.results); err != nil {
		t.Fatal(err)
	}
}
