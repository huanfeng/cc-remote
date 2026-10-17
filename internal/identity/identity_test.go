package identity

import (
	"strings"
	"testing"
)

func TestIdentity(t *testing.T) {
	dir := t.TempDir()
	id, err := Load(dir)
	if err != nil || id != (Identity{}) {
		t.Fatalf("missing file: %+v %v", id, err)
	}
	name, hex := id.Resolved()
	if name != DefaultName() || hex != hexOf(DefaultColor(name)) {
		t.Fatalf("defaults %q %q", name, hex)
	}
	if err = Save(dir, Identity{Name: " pdev ", Color: "Teal"}); err != nil {
		t.Fatal(err)
	}
	if id, _ = Load(dir); id != (Identity{Name: "pdev", Color: "teal"}) {
		t.Fatalf("saved %+v", id)
	}
	if name, hex = id.Resolved(); name != "pdev" || hex != "#0f5e5c" {
		t.Fatalf("resolved %q %q", name, hex)
	}
	if _, hex = (Identity{Color: "#A1B2C3"}).Resolved(); hex != "#a1b2c3" {
		t.Fatalf("hex %q", hex)
	}
	for _, bad := range []Identity{{Color: "chartreuse"}, {Name: strings.Repeat("x", 41)}, {Name: "a\nb"}} {
		if Save(dir, bad) == nil {
			t.Errorf("%+v saved", bad)
		}
	}
}
