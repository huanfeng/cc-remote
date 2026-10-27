package codex

import (
	"encoding/json"
	"testing"

	"cc-remote/internal/claude"
)

// Notification fields verified against the installed CLI's generated v2 schema.
func TestAdapterActivityNotifications(t *testing.T) {
	var events []claude.Event
	p := &Process{threadID: "thread-a", hooks: claude.ProcessHooks{Event: func(event claude.Event) error {
		events = append(events, event)
		return nil
	}}}
	methods := []string{"item/reasoning/summaryTextDelta", "item/reasoning/summaryPartAdded", "item/commandExecution/outputDelta", "item/fileChange/outputDelta", "item/mcpToolCall/progress"}
	for _, method := range methods {
		payload := json.RawMessage(`{"threadId":"thread-a","turnId":"turn-a","itemId":"item-a","summaryIndex":1,"delta":"read code","message":"running search"}`)
		if err := p.handle(rpcMessage{Method: method, Params: payload}); err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(events[len(events)-1].Payload, &got); err != nil {
			t.Fatal(err)
		}
		if got["subtype"] != method || got["threadId"] != "thread-a" || got["turnId"] != "turn-a" || got["itemId"] != "item-a" {
			t.Fatalf("lost activity identity: %#v", got)
		}
	}
	before := len(events)
	for _, message := range []rpcMessage{
		{Method: "item/reasoning/textDelta", Params: json.RawMessage(`{"threadId":"thread-a","delta":"not a public summary"}`)},
		{Method: "item/reasoning/summaryTextDelta", Params: json.RawMessage(`{"threadId":"other-thread","delta":"other session"}`)},
	} {
		if err := p.handle(message); err != nil {
			t.Fatal(err)
		}
	}
	if len(events) != before {
		t.Fatal("raw reasoning or another thread's activity leaked")
	}
}
