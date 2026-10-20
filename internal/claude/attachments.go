package claude

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"
)

// AttachmentNote follows a web message's text in the prompt Claude gets, before
// one JSON line per attached file ({"path","name","mime","size"}).
const AttachmentNote = "\n\nThe user attached files stored on this host. Use the Read tool to open each attachment before answering (Read supports text, PDF, images and Jupyter notebooks); for other binary formats, inspect them with suitable tools. Attachments (one JSON object per line; path is the absolute host path, name is the original file name):\n"

// An upload is stored as <upload dir>/<session id>/<upload id>.<png|jpeg|gif>
// (an image) or <upload id>-<name> (any other file).
var uploadFile = regexp.MustCompile(`^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})(\.(png|jpeg|gif)|-.+)$`)
var uploadSession = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type promptAttachment struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// SplitAttachments parts a prompt sent from the web into the text the user
// typed and the files attached to it, as the web shows them (a payload of
// {"attachments": [...]}, each served by the host at its url). A prompt
// without attachments comes back as is, with no payload; so do lines that are
// not uploads, kept in the text.
func SplitAttachments(prompt string) (string, json.RawMessage) {
	text, list, ok := strings.Cut(strings.ReplaceAll(prompt, "\r\n", "\n"), AttachmentNote)
	if !ok {
		return prompt, nil
	}
	var items []promptAttachment
	for _, line := range strings.Split(strings.TrimSpace(list), "\n") {
		var f struct {
			Path, Name, MIME string
			Size             int
		}
		if json.Unmarshal([]byte(line), &f) != nil {
			return prompt, nil
		}
		// Either separator: the transcript may come from a host on another system.
		dir, file := path.Split(strings.ReplaceAll(f.Path, `\`, "/"))
		session := path.Base(strings.TrimSuffix(dir, "/"))
		m := uploadFile.FindStringSubmatch(file)
		if m == nil || !uploadSession.MatchString(session) {
			return prompt, nil
		}
		kind := "file"
		if m[3] != "" {
			kind = "image"
		}
		items = append(items, promptAttachment{ID: m[1], Kind: kind, Name: f.Name, MIME: f.MIME, Size: f.Size, URL: "/api/sessions/" + session + "/uploads/" + m[1]})
	}
	if len(items) == 0 {
		return prompt, nil
	}
	payload, _ := json.Marshal(map[string]any{"attachments": items})
	return text, payload
}
