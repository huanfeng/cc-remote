package claude

import "testing"

// Claude Code's recap (written while its user was away) becomes a recap event.
func TestTranscriptRecap(t *testing.T) {
	var session NativeSession
	line := []byte(`{"type":"system","subtype":"away_summary","content":"分析完了，下一步换一只 U 盘重跑。 (disable recaps in /config)","isMeta":false}`)
	events := transcriptEvents(line, &session)
	if len(events) != 1 || events[0].Kind != "recap" || events[0].Text != "分析完了，下一步换一只 U 盘重跑。" {
		t.Fatalf("recap events = %+v", events)
	}
	for _, other := range []string{
		`{"type":"system","subtype":"turn_duration","durationMs":5}`,
		`{"type":"system","subtype":"away_summary","content":"(disable recaps in /config)"}`,
		`{"type":"system","subtype":"away_summary","content":"x","isSidechain":true}`,
	} {
		if events := transcriptEvents([]byte(other), &session); len(events) != 0 {
			t.Fatalf("%s gave %+v", other, events)
		}
	}
}
