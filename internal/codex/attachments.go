package codex

import (
	"encoding/json"
	"strings"

	"cc-remote/internal/claude"
)

const attachmentNote = "\n\nThe user attached files stored on this host. Inspect each attachment before answering, using the available tools appropriate to its format. Image attachments are also supplied as image inputs. Attachments (one JSON object per line; path is the absolute host path, name is the original file name):\n"

// turnInput translates only the host's recognized attachment envelope. An
// arbitrary marker or malformed JSON remains verbatim user text.
func turnInput(prompt string) []any {
	input := []any{map[string]any{"type": "text", "text": prompt}}
	_, payload := claude.SplitAttachments(prompt)
	if len(payload) == 0 {
		return input
	}
	var recognized struct {
		Attachments []struct {
			Kind string `json:"kind"`
		} `json:"attachments"`
	}
	if json.Unmarshal(payload, &recognized) != nil {
		return input
	}
	text, list, ok := strings.Cut(strings.ReplaceAll(prompt, "\r\n", "\n"), claude.AttachmentNote)
	if !ok {
		return input
	}
	lines := strings.Split(strings.TrimSpace(list), "\n")
	if len(lines) != len(recognized.Attachments) {
		return input
	}
	var images []any
	for i, line := range lines {
		var file struct {
			Path string `json:"path"`
			MIME string `json:"mime"`
		}
		if json.Unmarshal([]byte(line), &file) != nil {
			return input
		}
		if recognized.Attachments[i].Kind == "image" || strings.HasPrefix(strings.ToLower(file.MIME), "image/") {
			images = append(images, map[string]any{"type": "localImage", "path": file.Path})
		}
	}
	input[0] = map[string]any{"type": "text", "text": text + attachmentNote + list}
	return append(input, images...)
}
