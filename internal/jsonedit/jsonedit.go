// Package jsonedit changes single members of a JSON object document in place.
// Bytes outside the edited member are kept verbatim, so key order, indentation,
// newline style and unrelated formatting survive the edit.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

type member struct {
	key                                string
	keyStart, keyEnd, valStart, valEnd int
}

type object struct {
	start, end int // offsets of '{' and '}'
	members    []member
}

// BOM is the UTF-8 byte order mark some Windows editors prepend.
var BOM = []byte("\xEF\xBB\xBF")

// StripBOM returns doc without a leading UTF-8 byte order mark.
func StripBOM(doc []byte) []byte {
	return bytes.TrimPrefix(doc, BOM)
}

// Set writes value (raw JSON) at the object path, creating parent objects as needed.
// A nil value removes the member; a parent object left empty by removal is removed too.
// Duplicate members along the path are collapsed: JSON parsers use the last one, so
// earlier duplicates are dropped and the last is edited (or removed with the rest).
// A leading UTF-8 BOM is tolerated and kept.
func Set(doc []byte, path []string, value json.RawMessage) ([]byte, error) {
	if len(path) == 0 {
		return nil, errors.New("empty path")
	}
	if value != nil && !json.Valid(value) {
		return nil, errors.New("invalid value")
	}
	if bytes.HasPrefix(doc, BOM) {
		out, err := Set(doc[len(BOM):], path, value)
		if err != nil {
			return nil, err
		}
		return append(append([]byte(nil), BOM...), out...), nil
	}
	if len(bytes.TrimSpace(doc)) == 0 {
		if value == nil {
			return doc, nil
		}
		doc = []byte("{}\n")
	}
	if !json.Valid(doc) {
		return nil, errors.New("document is not valid JSON")
	}
	i := skipSpace(doc, 0)
	if doc[i] != '{' {
		return nil, errors.New("document is not a JSON object")
	}
	out := append([]byte(nil), doc...)
	return set(out, i, path, value)
}

// dropEarlierDuplicates removes every member named key except the last one,
// which is the effective member, and returns the re-parsed object and its index.
func dropEarlierDuplicates(doc []byte, at int, key string) ([]byte, object, int) {
	for {
		o := parseObject(doc, at)
		first, idx := -1, -1
		for i, m := range o.members {
			if m.key == key {
				if first < 0 {
					first = i
				}
				idx = i
			}
		}
		if first == idx {
			return doc, o, idx
		}
		doc = remove(doc, o, first)
	}
}

func set(doc []byte, at int, path []string, value json.RawMessage) ([]byte, error) {
	doc, o, idx := dropEarlierDuplicates(doc, at, path[0])
	if len(path) == 1 {
		switch {
		case value == nil && idx < 0:
			return doc, nil
		case value == nil:
			return remove(doc, o, idx), nil
		case idx >= 0:
			m := o.members[idx]
			return splice(doc, m.valStart, m.valEnd, value), nil
		default:
			return insert(doc, o, path[0], value), nil
		}
	}
	if idx < 0 {
		if value == nil {
			return doc, nil
		}
		doc = insert(doc, o, path[0], json.RawMessage("{}"))
		o = parseObject(doc, at)
		idx = len(o.members) - 1
	}
	m := o.members[idx]
	if doc[m.valStart] != '{' {
		return nil, errors.New(path[0] + " is not an object")
	}
	doc, err := set(doc, m.valStart, path[1:], value)
	if err != nil || value != nil {
		return doc, err
	}
	o = parseObject(doc, at)
	m = o.members[idx]
	if len(parseObject(doc, m.valStart).members) == 0 {
		doc = remove(doc, o, idx)
	}
	return doc, nil
}

func splice(doc []byte, start, end int, with []byte) []byte {
	out := make([]byte, 0, len(doc)-(end-start)+len(with))
	out = append(out, doc[:start]...)
	out = append(out, with...)
	return append(out, doc[end:]...)
}

func remove(doc []byte, o object, idx int) []byte {
	m := o.members[idx]
	switch {
	case len(o.members) == 1:
		return splice(doc, o.start+1, o.end, nil)
	case idx < len(o.members)-1:
		// Drop "key": value, and the whitespace before the next key; the next key inherits this indent.
		return splice(doc, m.keyStart, o.members[idx+1].keyStart, nil)
	default:
		return splice(doc, o.members[idx-1].valEnd, m.valEnd, nil)
	}
}

func insert(doc []byte, o object, key string, value json.RawMessage) []byte {
	k, _ := json.Marshal(key)
	nl := newline(doc)
	if n := len(o.members); n > 0 {
		last := o.members[n-1]
		// Leading whitespace of the last key: either a newline plus indent, or inline spacing.
		lead := string(doc[prevNonSpace(doc, last.keyStart)+1 : last.keyStart])
		if i := strings.LastIndexAny(lead, "\n"); i >= 0 {
			lead = nl + lead[i+1:]
		}
		colon := doc[last.keyEnd:last.valStart]
		text := append([]byte(","+lead), k...)
		text = append(append(text, colon...), value...)
		return splice(doc, last.valEnd, last.valEnd, text)
	}
	if !bytes.Contains(doc, []byte("\n")) {
		return splice(doc, o.start+1, o.end, append(append(k, ':'), value...))
	}
	base := lineIndent(doc, o.start)
	text := "{" + nl + base + indentUnit(doc) + string(k) + ": " + string(value) + nl + base + "}"
	return splice(doc, o.start, o.end+1, []byte(text))
}

func newline(doc []byte) string {
	if bytes.Contains(doc, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// indentUnit uses the indentation of the first indented line, defaulting to two spaces.
func indentUnit(doc []byte) string {
	for _, line := range strings.Split(string(doc), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) < len(line) && strings.TrimSpace(trimmed) != "" {
			return line[:len(line)-len(trimmed)]
		}
	}
	return "  "
}

func lineIndent(doc []byte, at int) string {
	start := bytes.LastIndexByte(doc[:at], '\n') + 1
	end := start
	for end < at && (doc[end] == ' ' || doc[end] == '\t') {
		end++
	}
	return string(doc[start:end])
}

func prevNonSpace(doc []byte, at int) int {
	i := at - 1
	for i >= 0 && isSpace(doc[i]) {
		i--
	}
	return i
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func skipSpace(doc []byte, i int) int {
	for i < len(doc) && isSpace(doc[i]) {
		i++
	}
	return i
}

// parseObject assumes doc is valid JSON and doc[at] == '{'.
func parseObject(doc []byte, at int) object {
	o := object{start: at}
	i := skipSpace(doc, at+1)
	for doc[i] != '}' {
		if doc[i] == ',' {
			i = skipSpace(doc, i+1)
		}
		m := member{keyStart: i}
		m.keyEnd = skipValue(doc, i)
		_ = json.Unmarshal(doc[m.keyStart:m.keyEnd], &m.key)
		i = skipSpace(doc, m.keyEnd)
		i = skipSpace(doc, i+1) // ':'
		m.valStart = i
		m.valEnd = skipValue(doc, i)
		o.members = append(o.members, m)
		i = skipSpace(doc, m.valEnd)
	}
	o.end = i
	return o
}

// skipValue returns the offset just past the JSON value starting at i.
func skipValue(doc []byte, i int) int {
	switch doc[i] {
	case '"':
		for j := i + 1; j < len(doc); j++ {
			if doc[j] == '\\' {
				j++
			} else if doc[j] == '"' {
				return j + 1
			}
		}
		return len(doc)
	case '{', '[':
		depth := 0
		for j := i; j < len(doc); j++ {
			switch doc[j] {
			case '"':
				j = skipValue(doc, j) - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return len(doc)
	default:
		j := i
		for j < len(doc) && !isSpace(doc[j]) && doc[j] != ',' && doc[j] != '}' && doc[j] != ']' {
			j++
		}
		return j
	}
}
