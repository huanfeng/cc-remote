package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestProjectShellFlowKeepsInterruptIndependentOfOutput(t *testing.T) {
	flood := "while :; do printf '%01024d\\n' 0; sleep 0.002; done"
	prompt := []byte("$ ")
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh.exe"); err != nil {
			if _, err := exec.LookPath("powershell.exe"); err != nil {
				t.Skip("PowerShell is required for the interactive flood fixture")
			}
		}
		flood = "while ($true) { [Console]::WriteLine(('0' * 1024)); Start-Sleep -Milliseconds 2 }"
		prompt = []byte("PS ")
	} else {
		t.Setenv("SHELL", "/bin/sh")
	}
	s, host, _ := setup(t, nil)
	defer closeShellTestHost(t, s)
	s.cfg.DataDir = t.TempDir()
	code, body := request(t, host, "POST", "/api/project-shells", map[string]string{"cwd": t.TempDir()}, testToken)
	var view projectShellView
	if code != http.StatusOK || json.Unmarshal(body, &view) != nil || view.RunID == "" {
		t.Fatalf("open Shell: %d %s", code, body)
	}
	path := "/api/project-shells/" + view.ID + "/runs/" + view.RunID
	code, body = request(t, host, "POST", path+"/ticket", nil, testToken)
	var ticket struct{ Ticket string }
	if code != http.StatusOK || json.Unmarshal(body, &ticket) != nil || ticket.Ticket == "" {
		t.Fatalf("issue ticket: %d %s", code, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	streamURL := "ws" + strings.TrimPrefix(host.URL, "http") + path + "/stream?flow=1&after=0&ticket=" + url.QueryEscape(ticket.Ticket)
	ws, _, err := websocket.Dial(ctx, streamURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	ws.SetReadLimit(projectShellOutputWindow)
	ack := func(seq uint64) {
		t.Helper()
		if err := ws.Write(ctx, websocket.MessageText, fmt.Appendf(nil, `{"type":"ack","seq":%d}`, seq)); err != nil {
			t.Fatal(err)
		}
	}
	keys := func(data string) {
		t.Helper()
		if err := ws.Write(ctx, websocket.MessageBinary, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	read := func() (projectShellFrame, []byte) {
		t.Helper()
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var frame projectShellFrame
		if kind == websocket.MessageBinary && len(data) > 8 {
			seq := binary.BigEndian.Uint64(data)
			return projectShellFrame{Type: "output", Seq: &seq}, data[8:]
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatal(err)
		}
		return frame, nil
	}
	if frame, _ := read(); frame.Type != "flow" {
		t.Fatalf("flow was not negotiated: %+v", frame)
	}
	for {
		frame, _ := read()
		if frame.Seq != nil && (frame.Type == "output" || frame.Type == "size" || frame.Type == "ready") {
			ack(*frame.Seq)
		}
		if frame.Type == "control" && frame.Held != nil && *frame.Held {
			break
		}
	}
	keys(flood + "\r")
	var heldCursor uint64
	for heldCursor == 0 {
		frame, _ := read()
		if frame.Type == "output" {
			heldCursor = *frame.Seq
		}
	}
	s.projectShells.mu.Lock()
	pty := s.projectShells.entries[view.ID].run.pty
	s.projectShells.mu.Unlock()
	// Leave the socket credit outstanding. The foreground program must produce
	// output and handle Ctrl+C even though the client is not acknowledging it.
	readHostOutput := func(after uint64) []byte {
		var output bytes.Buffer
		for _, e := range pty.ReadOutput(after, 0).Events {
			output.Write(e.Data)
		}
		return output.Bytes()
	}
	await(t, func() bool { return bytes.Contains(readHostOutput(heldCursor), bytes.Repeat([]byte{'0'}, 80)) })
	interruptCursor := pty.ReadOutput(heldCursor, 1).Latest
	keys("\x03")
	interrupted := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(15 * time.Millisecond) {
		output := readHostOutput(interruptCursor)
		if bytes.Contains(output, prompt) || (runtime.GOOS != "windows" && bytes.Contains(output, []byte("# "))) {
			interrupted = true
			break
		}
	}
	if !interrupted {
		output := readHostOutput(interruptCursor)
		t.Fatalf("Ctrl+C did not return to the prompt (%d bytes after key; final bytes %q)", len(output), output[max(0, len(output)-256):])
	}
	const marker = "ccr-flow-interrupted"
	keys("echo " + marker + "\r")
	await(t, func() bool { return bytes.Contains(readHostOutput(interruptCursor), []byte(marker)) })
	ack(heldCursor)
	var output bytes.Buffer
	for !bytes.Contains(output.Bytes(), []byte(marker)) {
		frame, data := read()
		if frame.Type == "reset" {
			t.Fatal("lost output while the client paused acknowledgements")
		}
		if frame.Seq != nil {
			ack(*frame.Seq)
		}
		output.Write(data)
	}
}
