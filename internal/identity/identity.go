// Package identity is a host's name, colour and icon, which tell several hosts
// apart in the browser: the tab title and icon, the sidebar and the login page.
// They are stored in <data dir>/identity.json; empty fields take defaults (the
// machine's host name, a colour derived from the name, and the CC Remote logo).
package identity

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"cc-remote/internal/i18n"
)

// Identity is what the user set; empty fields follow the defaults.
type Identity struct {
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
	// Icon is the glyph on the colour: a preset's name (web/public/host-icons.json),
	// or a few characters or an emoji drawn as text.
	Icon string `json:"icon,omitempty"`
}

// MaxIconLength is the most characters (code points) an icon may have: a
// short text, or an emoji built from several code points.
const MaxIconLength = 8

const fileName = "identity.json"

// Palette is the colours to choose from: dark tones that keep the light logo
// glyph readable. The first is the original CC Remote green.
var Palette = []struct{ Key, Hex string }{
	{"green", "#293c2e"}, {"teal", "#0f5e5c"}, {"blue", "#1f4e8c"}, {"indigo", "#3b3f8f"}, {"purple", "#5b3a87"},
	{"pink", "#8a2f62"}, {"red", "#9b2c2c"}, {"orange", "#a4501c"}, {"amber", "#7d6210"}, {"slate", "#3d4650"},
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Load reads the identity; a missing file is an empty identity.
func Load(dataDir string) (Identity, error) {
	var id Identity
	b, err := os.ReadFile(filepath.Join(dataDir, fileName))
	if errors.Is(err, os.ErrNotExist) {
		return id, nil
	}
	if err != nil {
		return id, err
	}
	if err = json.Unmarshal(b, &id); err != nil {
		return Identity{}, i18n.Errorf("identity.json is not valid JSON: %w", "identity.json 不是有效 JSON：%w", err)
	}
	return id, nil
}

// Save validates and writes the identity (0600, replaced atomically).
func Save(dataDir string, id Identity) error {
	id.Name, id.Color, id.Icon = strings.TrimSpace(id.Name), strings.ToLower(strings.TrimSpace(id.Color)), strings.TrimSpace(id.Icon)
	if err := id.Validate(); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(id, "", "  ")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dataDir, ".identity-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dataDir, fileName))
}

// Validate accepts a name of up to 40 characters without control characters,
// a palette colour or #rrggbb, and an icon of up to MaxIconLength characters
// (or empty fields).
func (id Identity) Validate() error {
	if len([]rune(id.Name)) > 40 || strings.IndexFunc(id.Name, unicode.IsControl) >= 0 {
		return i18n.NewError("the host name must be at most 40 characters", "主机名称最多 40 个字符")
	}
	if len([]rune(id.Icon)) > MaxIconLength || strings.IndexFunc(id.Icon, unicode.IsControl) >= 0 {
		return i18n.NewError("the icon must be a preset name, or at most %d characters or an emoji", "图标只能是预设名称，或最多 %d 个字符或一个 emoji", MaxIconLength)
	}
	if id.Color != "" && hexOf(id.Color) == "" {
		return i18n.NewError("the color must be one of %s or #rrggbb", "颜色只能是 %s 之一，或 #rrggbb", Keys())
	}
	return nil
}

// Keys lists the palette's colour names.
func Keys() string {
	var keys []string
	for _, c := range Palette {
		keys = append(keys, c.Key)
	}
	return strings.Join(keys, ", ")
}

func hexOf(color string) string {
	color = strings.ToLower(strings.TrimSpace(color))
	for _, c := range Palette {
		if c.Key == color {
			return c.Hex
		}
	}
	if hexColor.MatchString(color) {
		return color
	}
	return ""
}

// DefaultName is the machine's host name without its domain.
func DefaultName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "CC Remote"
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	return name
}

// DefaultColor is a palette colour picked by the name, so hosts left at their
// defaults usually differ.
func DefaultColor(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return Palette[h.Sum32()%uint32(len(Palette))].Key
}

// Resolved is the name and colour (#rrggbb) to show.
func (id Identity) Resolved() (name, hex string) {
	name = id.Name
	if name == "" {
		name = DefaultName()
	}
	if hex = hexOf(id.Color); hex == "" {
		hex = hexOf(DefaultColor(name))
	}
	return name, hex
}
