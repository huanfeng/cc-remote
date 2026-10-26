package codex

import (
	"errors"
	"testing"
	"time"
)

func TestSteerTargetsActiveTurnWithoutStartingAnother(t *testing.T) {
	p, l := startFake(t, fakeOptions(t))
	if err := p.Steer("too early"); !errors.Is(err, ErrSteerRejected) {
		t.Fatalf("idle steer: %v", err)
	}
	if err := p.Send("hold"); err != nil {
		t.Fatal(err)
	}
	receive(t, l.replays)
	deadline := time.Now().Add(3 * time.Second)
	for {
		p.mu.Lock()
		active := p.turnID != ""
		p.mu.Unlock()
		if active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := p.Steer("focus on tests"); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-l.replays:
		t.Fatalf("steer consumed the queue: %q", text)
	default:
	}
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, l.results); err == nil {
		t.Fatal("expected interruption")
	}
	if err := p.Steer("too late"); !errors.Is(err, ErrSteerRejected) {
		t.Fatalf("completed steer: %v", err)
	}
}

func TestSteerRejectsCommands(t *testing.T) {
	p, _ := startFake(t, fakeOptions(t))
	for _, text := range []string{"", "  ", "/compact"} {
		if err := p.Steer(text); !errors.Is(err, ErrSteerRejected) {
			t.Fatalf("accepted %q: %v", text, err)
		}
	}
}

func TestSteerDistinguishesRejectionFromUncertainDelivery(t *testing.T) {
	for _, mode := range []string{"steer-reject", "steer-wrong-ack", "steer-disconnect"} {
		t.Run(mode, func(t *testing.T) {
			o := fakeOptions(t)
			t.Setenv("CODEX_ADAPTER_TEST_MODE", mode)
			p, l := startFake(t, o)
			if err := p.Send("hold"); err != nil {
				t.Fatal(err)
			}
			receive(t, l.replays)
			deadline := time.Now().Add(3 * time.Second)
			for {
				p.mu.Lock()
				active := p.turnID != ""
				p.mu.Unlock()
				if active {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("no active turn")
				}
				time.Sleep(time.Millisecond)
			}
			err := p.Steer("extra")
			if err == nil || errors.Is(err, ErrSteerRejected) != (mode == "steer-reject") {
				t.Fatalf("delivery classification: %v", err)
			}
			if mode == "steer-reject" {
				if err := p.Interrupt(); err != nil {
					t.Fatal(err)
				}
			} else {
				receive(t, p.Done())
			}
		})
	}
}
