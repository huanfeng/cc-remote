package claude

import (
	"reflect"
	"slices"
	"time"
)

// AskWait is how long a terminal's permission dialog (a tool approval,
// AskUserQuestion or ExitPlanMode) stays answerable from the web: a week, so a
// question asked while the user is away can still be answered remotely when they
// come back. The host gives up first, then the hook, then Claude Code's hook
// timeout, so a long wait is recorded as expired rather than as answered in the
// terminal.
const AskWait = 7 * 24 * time.Hour

// AskHookTimeout is the PermissionRequest hook's timeout in settings (seconds).
const AskHookTimeout = int((AskWait + 2*time.Minute) / time.Second)

// SameCall reports whether a tool_use input from the transcript is the call a
// PermissionRequest hook was asked about. AskUserQuestion compares its question
// strings (key order and defaulted fields differ), ExitPlanMode's transcript
// input is empty so the tool alone matches, and any other tool must agree on
// every field both inputs carry.
func SameCall(tool string, hook, transcript map[string]any) bool {
	switch tool {
	case "AskUserQuestion":
		return slices.Equal(QuestionTexts(hook), QuestionTexts(transcript))
	case "ExitPlanMode":
		return true
	}
	shared := 0
	for k, v := range hook {
		if w, ok := transcript[k]; ok {
			if !reflect.DeepEqual(v, w) {
				return false
			}
			shared++
		}
	}
	return shared > 0 || len(hook) == 0 && len(transcript) == 0
}

// QuestionTexts identifies an AskUserQuestion call by its question strings.
func QuestionTexts(input map[string]any) []string {
	list, _ := input["questions"].([]any)
	out := make([]string, 0, len(list))
	for _, q := range list {
		m, _ := q.(map[string]any)
		text, _ := m["question"].(string)
		out = append(out, text)
	}
	return out
}
