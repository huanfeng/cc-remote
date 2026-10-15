package service

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// TaskName is the Windows Task Scheduler task name.
const TaskName = "CC Remote"

// TaskSpec describes the Windows scheduled task action.
type TaskSpec struct {
	UserID, Command, Arguments, WorkingDir string
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// TaskXML renders a user-context logon task. Task Scheduler restarts only
// when the action fails to launch; the supervisor handles host crashes.
func TaskXML(spec TaskSpec) string {
	user := xmlText(spec.UserID)
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>CC Remote host (cc-remote service)</Description>
    <URI>\` + xmlText(TaskName) + `</URI>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + user + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + user + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlText(spec.Command) + `</Command>
      <Arguments>` + xmlText(spec.Arguments) + `</Arguments>
      <WorkingDirectory>` + xmlText(spec.WorkingDir) + `</WorkingDirectory>
    </Exec>
  </Actions>
</Task>
`
}

// EncodeUTF16LE returns s as UTF-16 LE with a byte order mark, as schtasks /XML requires.
func EncodeUTF16LE(s string) []byte {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	units := utf16.Encode([]rune(s))
	b := make([]byte, 2+2*len(units))
	b[0], b[1] = 0xFF, 0xFE
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2+2*i:], u)
	}
	return b
}

// WindowsQuote quotes one argument for CommandLineToArgvW.
func WindowsQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\v\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		switch r {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
			b.WriteRune(r)
			slashes = 0
			continue
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			slashes = 0
			b.WriteRune(r)
			continue
		}
	}
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}

// WindowsCommandLine joins arguments with WindowsQuote.
func WindowsCommandLine(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = WindowsQuote(a)
	}
	return strings.Join(q, " ")
}

// SystemdQuote quotes a word for unit files; expand also escapes "$" for ExecStart.
func SystemdQuote(s string, execLine bool) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "\n", `\n`)
	s = r.Replace(s)
	if execLine {
		s = strings.ReplaceAll(s, "$", "$$")
	}
	return `"` + s + `"`
}

// SystemdPath renders a path setting such as WorkingDirectory=. Unlike
// ExecStart= and Environment=, systemd takes these values literally: quotes
// are not removed (a quoted path is "not absolute" and the unit is rejected),
// so only the "%" specifier character is escaped.
func SystemdPath(s string) string {
	return strings.ReplaceAll(s, "%", "%%")
}

// SystemdUnit renders the user unit running exe with args.
func SystemdUnit(exe string, args []string, env map[string]string) string {
	words := []string{SystemdQuote(exe, true)}
	for _, a := range args {
		words = append(words, SystemdQuote(a, true))
	}
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=CC Remote host\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\n")
	for _, k := range sortedKeys(env) {
		b.WriteString("Environment=" + SystemdQuote(k+"="+env[k], false) + "\n")
	}
	b.WriteString("ExecStart=" + strings.Join(words, " ") + "\n")
	b.WriteString("Restart=on-failure\nRestartSec=5\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// LaunchdLabel is the macOS LaunchAgent label.
const LaunchdLabel = "com.cc-remote.host"

// LaunchdPlist renders a LaunchAgent that restarts the host only after a failed exit.
func LaunchdPlist(exe string, args []string, env map[string]string, stdoutPath, workDir string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + LaunchdLabel + `</string>
  <key>ProgramArguments</key>
  <array>
`)
	for _, a := range append([]string{exe}, args...) {
		b.WriteString("    <string>" + xmlText(a) + "</string>\n")
	}
	b.WriteString("  </array>\n  <key>EnvironmentVariables</key>\n  <dict>\n")
	for _, k := range sortedKeys(env) {
		b.WriteString("    <key>" + xmlText(k) + "</key>\n    <string>" + xmlText(env[k]) + "</string>\n")
	}
	b.WriteString(`  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>WorkingDirectory</key>
  <string>` + xmlText(workDir) + `</string>
  <key>StandardOutPath</key>
  <string>` + xmlText(stdoutPath) + `</string>
  <key>StandardErrorPath</key>
  <string>` + xmlText(stdoutPath) + `</string>
  <key>ProcessType</key>
  <string>Background</string>
</dict>
</plist>
`)
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// HostArgs builds the host command line for a service manager.
func HostArgs(cfg Config, manager string) []string {
	var args []string
	for _, l := range cfg.Listen {
		args = append(args, "--listen", l)
	}
	args = append(args, "--data-dir", cfg.DataDir, "--claude", cfg.Claude)
	if cfg.ClaudeConfigDir != "" {
		args = append(args, "--claude-config-dir", cfg.ClaudeConfigDir)
	}
	if cfg.AllowInsecureRemote {
		args = append(args, "--allow-insecure-remote")
	}
	return append(args, "--log-file", cfg.LogFile, "--service-manager", manager)
}

// SetPESubsystem rewrites the PE optional header subsystem (2 = GUI, 3 = console).
func SetPESubsystem(b []byte, subsystem uint16) error {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return errors.New("not a PE executable")
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3C:]))
	off := pe + 4 + 20 + 68
	if pe < 0 || off+2 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
		return fmt.Errorf("invalid PE header")
	}
	binary.LittleEndian.PutUint16(b[off:], subsystem)
	return nil
}

// WindowsCmdQuote quotes a path inside a .cmd script.
func WindowsCmdQuote(s string) string {
	return `"` + strings.ReplaceAll(s, "%", "%%") + `"`
}

// ShellQuote quotes a word for POSIX sh.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
