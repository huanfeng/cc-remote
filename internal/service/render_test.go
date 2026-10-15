package service

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestTaskXML(t *testing.T) {
	spec := TaskSpec{UserID: `PC\Me & You`, Command: `C:\Users\Me\AppData\Local\cc-remote\bin\cc-remote-service.exe`, Arguments: WindowsCommandLine([]string{"service", "supervise", "--data-dir", `C:\Users\Me\My Data`}), WorkingDir: `C:\Users\Me`}
	doc := TaskXML(spec)
	for _, want := range []string{
		"<LogonTrigger>", "<UserId>PC\\Me &amp; You</UserId>", "<LogonType>InteractiveToken</LogonType>",
		"<RunLevel>LeastPrivilege</RunLevel>", "<Hidden>true</Hidden>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<Interval>PT1M</Interval>", "<Count>999</Count>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		"<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>", "<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",
		`--data-dir &#34;C:\Users\Me\My Data&#34;`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("task XML missing %q", want)
		}
	}
	// Well-formed apart from the declared encoding, which describes the file bytes.
	dec := xml.NewDecoder(strings.NewReader(strings.Replace(doc, `encoding="UTF-16"`, "", 1)))
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() != "EOF" {
				t.Fatalf("invalid XML: %v", err)
			}
			break
		}
	}
	b := EncodeUTF16LE(doc)
	if b[0] != 0xFF || b[1] != 0xFE || len(b)%2 != 0 {
		t.Fatalf("missing UTF-16 LE BOM: % x", b[:4])
	}
	units := make([]uint16, (len(b)-2)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2+2*i:])
	}
	text := string(utf16.Decode(units))
	if !strings.HasPrefix(text, "<?xml") || !strings.Contains(text, "\r\n<Task") {
		t.Fatalf("decoded text = %q", text[:40])
	}
}

func TestWindowsQuote(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		"":             `""`,
		`C:\a b\`:      `"C:\a b\\"`,
		`say "hi"`:     `"say \"hi\""`,
		`C:\no\spaces`: `C:\no\spaces`,
		`a\"b c`:       `"a\\\"b c"`,
	}
	for in, want := range cases {
		if got := WindowsQuote(in); got != want {
			t.Errorf("WindowsQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSystemdUnitQuoting(t *testing.T) {
	cfg := Config{Listen: []string{"127.0.0.1:8787", "lan:8787"}, DataDir: "/home/me/My Data", Claude: "/opt/claude bin/claude", LogFile: "/home/me/My Data/host.log"}
	unit := SystemdUnit("/home/me/.local/share/cc-remote/bin/cc-remote", HostArgs(cfg, "systemd"), map[string]string{"CC_REMOTE_SERVICE_MANAGER": "systemd", "PATH": "/usr/bin:/home/me/50%"})
	for _, want := range []string{
		`ExecStart="/home/me/.local/share/cc-remote/bin/cc-remote" "--listen" "127.0.0.1:8787" "--listen" "lan:8787" "--data-dir" "/home/me/My Data" "--claude" "/opt/claude bin/claude"`,
		`"--log-file" "/home/me/My Data/host.log" "--service-manager" "systemd"`,
		`Environment="CC_REMOTE_SERVICE_MANAGER=systemd"`,
		`Environment="PATH=/usr/bin:/home/me/50%%"`,
		"Type=simple", "Restart=on-failure", "RestartSec=5", "WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q\n%s", want, unit)
		}
	}
	if got := SystemdQuote(`a"b\c$d`, true); got != `"a\"b\\c$$d"` {
		t.Errorf("SystemdQuote = %s", got)
	}
}

func TestLaunchdPlist(t *testing.T) {
	plist := LaunchdPlist("/Users/me/Library/Application Support/cc-remote/bin/cc-remote", []string{"--data-dir", "/Users/me/a&b <c>"}, map[string]string{"CC_REMOTE_SERVICE_MANAGER": "launchd", "PATH": "/opt/homebrew/bin:/usr/bin"}, "/Users/me/data/launchd.log", "/Users/me")
	for _, want := range []string{
		"<string>/Users/me/Library/Application Support/cc-remote/bin/cc-remote</string>",
		"<string>/Users/me/a&amp;b &lt;c&gt;</string>",
		"<key>CC_REMOTE_SERVICE_MANAGER</key>\n    <string>launchd</string>",
		"<key>SuccessfulExit</key>\n    <false/>",
		"<key>RunAtLoad</key>\n  <true/>",
		"<key>StandardErrorPath</key>\n  <string>/Users/me/data/launchd.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	dec := xml.NewDecoder(strings.NewReader(plist))
	dec.Strict = true
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() != "EOF" {
				t.Fatalf("invalid plist XML: %v", err)
			}
			break
		}
	}
}

func TestPathMerge(t *testing.T) {
	dir := `C:\Users\Me\AppData\Local\cc-remote\bin`
	merged, changed := MergePath(`%USERPROFILE%\bin;C:\Tools;`, dir, ";")
	if !changed || merged != `%USERPROFILE%\bin;C:\Tools;`+dir {
		t.Fatalf("merge = %q %v", merged, changed)
	}
	if again, changed := MergePath(merged, strings.ToUpper(dir)+`\`, ";"); changed || again != merged {
		t.Fatalf("duplicate added: %q", again)
	}
	if got, changed := MergePath("", dir, ";"); !changed || got != dir {
		t.Fatalf("empty merge = %q", got)
	}
	removed, changed := RemovePath(merged, dir+`\`, ";")
	if !changed || removed != `%USERPROFILE%\bin;C:\Tools` {
		t.Fatalf("remove = %q %v", removed, changed)
	}
	if !PathContains("/usr/bin:/home/me/.local/bin/", "/home/me/.local/bin", ":") {
		t.Fatal("unix PATH entry not found")
	}
	if PathContains("/usr/bin", "/USR/BIN", ":") {
		t.Fatal("unix PATH comparison must be case sensitive")
	}
}

func TestAliasScript(t *testing.T) {
	cmd := AliasScript("windows", `C:\Users\Me\100% stuff\cc-remote.exe`, []string{"--data-dir", `D:\data`})
	if cmd != "@echo off\r\n\"C:\\Users\\Me\\100%% stuff\\cc-remote.exe\" \"--data-dir\" \"D:\\data\" -- claude %*\r\n" {
		t.Fatalf("cmd = %q", cmd)
	}
	sh := AliasScript("linux", "/home/me/it's/cc-remote", nil)
	if !strings.HasPrefix(sh, "#!/bin/sh\n") || !strings.Contains(sh, `exec '/home/me/it'"'"'s/cc-remote' -- claude "$@"`) {
		t.Fatalf("sh = %q", sh)
	}
}

func TestSetPESubsystem(t *testing.T) {
	b := make([]byte, 0x200)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3C:], 0x80)
	copy(b[0x80:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[0x80+4+20+68:], 3)
	if err := SetPESubsystem(b, 2); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(b[0x80+4+20+68:]); got != 2 {
		t.Fatalf("subsystem = %d", got)
	}
	if err := SetPESubsystem([]byte("#!/bin/sh"), 2); err == nil {
		t.Fatal("non-PE accepted")
	}
}

func TestLastLines(t *testing.T) {
	if got := string(lastLines([]byte("a\nb\nc\n"), 2)); got != "b\nc\n" {
		t.Fatalf("got %q", got)
	}
	if got := string(lastLines([]byte("a\nb"), 5)); got != "a\nb\n" {
		t.Fatalf("got %q", got)
	}
}

func TestTailFollowsRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.log")
	os.WriteFile(path, []byte("one\ntwo\n"), 0600)
	var out syncBuffer
	stop := make(chan struct{})
	done := make(chan error)
	go func() { done <- tailLog(&out, path, 1, true, stop) }()
	time.Sleep(700 * time.Millisecond)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("three\n")
	f.Close()
	time.Sleep(700 * time.Millisecond)
	os.Rename(path, path+".1")
	os.WriteFile(path, []byte("x\n"), 0600)
	time.Sleep(700 * time.Millisecond)
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "two\nthree\nx\n" {
		t.Fatalf("got %q", got)
	}
}

type syncBuffer struct {
	mu chan struct{}
	b  bytes.Buffer
}

func (s *syncBuffer) lock() {
	if s.mu == nil {
		s.mu = make(chan struct{}, 1)
	}
	s.mu <- struct{}{}
}
func (s *syncBuffer) Write(p []byte) (int, error) {
	s.lock()
	defer func() { <-s.mu }()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.lock(); defer func() { <-s.mu }(); return s.b.String() }
