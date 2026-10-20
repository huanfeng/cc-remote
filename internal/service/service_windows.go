//go:build windows

package service

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"cc-remote/internal/i18n"

	"golang.org/x/sys/windows"
)

const managerName = "schtasks"

// The task runs a GUI-subsystem copy of the executable so no console window
// appears at logon; it supervises the console host started with CREATE_NO_WINDOW.
const serviceExeName = "cc-remote-service.exe"

func serviceExe() (string, error) {
	dir, err := BinDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, serviceExeName), nil
}

// decodeOutput converts schtasks output from the OEM code page (e.g. GBK).
func decodeOutput(b []byte) string {
	if utf8.Valid(b) || len(b) == 0 {
		return strings.TrimSpace(string(b))
	}
	const cpOEM = 1
	n, err := windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), nil, 0)
	if err != nil || n == 0 {
		return strings.TrimSpace(string(b))
	}
	w := make([]uint16, n)
	if _, err = windows.MultiByteToWideChar(cpOEM, 0, &b[0], int32(len(b)), &w[0], n); err != nil {
		return strings.TrimSpace(string(b))
	}
	return strings.TrimSpace(windows.UTF16ToString(w))
}

func runQuiet(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	b, err := cmd.CombinedOutput()
	out := decodeOutput(b)
	if err != nil {
		if out != "" {
			return out, fmt.Errorf("%s: %w: %s", name, err, out)
		}
		return out, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

func installBinaries(src string, dryRun bool, out io.Writer) error {
	dst, err := InstalledExe()
	if err != nil {
		return err
	}
	svc, err := serviceExe()
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] copy %s -> %s\n", "[dry-run] 复制 %s -> %s\n", src, dst))
		fmt.Fprint(out, i18n.F("[dry-run] copy %s -> %s (GUI subsystem, no console window)\n", "[dry-run] 复制 %s -> %s（GUI 子系统，不弹控制台窗口）\n", src, svc))
		return nil
	}
	if !samePath(src, dst) {
		if err := copyExecutable(src, dst, nil); err != nil {
			return err
		}
	}
	if err := copyExecutable(src, svc, func(b []byte) error { return SetPESubsystem(b, 2) }); err != nil {
		return err
	}
	fmt.Fprint(out, i18n.F("Installed %s\n", "已安装 %s\n", dst))
	return nil
}

func taskSpec(cfg *Config) (TaskSpec, error) {
	u, err := user.Current()
	if err != nil {
		return TaskSpec{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = cfg.DataDir
	}
	svc, err := serviceExe()
	if err != nil {
		return TaskSpec{}, err
	}
	args := append([]string{"service", "supervise"}, HostArgs(*cfg, managerName)...)
	return TaskSpec{UserID: u.Username, Command: svc, Arguments: WindowsCommandLine(args), WorkingDir: home}, nil
}

func register(cfg *Config, dryRun bool, out io.Writer) error {
	spec, err := taskSpec(cfg)
	if err != nil {
		return err
	}
	xml := TaskXML(spec)
	if dryRun {
		fmt.Fprint(out, i18n.F("[dry-run] schtasks /Create /TN %q /XML <temp>\\task.xml /F  (UTF-16 LE with BOM):\n%s", "[dry-run] schtasks /Create /TN %q /XML <临时目录>\\task.xml /F（带 BOM 的 UTF-16 LE）：\n%s", TaskName, xml))
		return nil
	}
	dir, err := os.MkdirTemp("", "cc-remote-task-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "task.xml")
	if err = os.WriteFile(path, EncodeUTF16LE(xml), 0600); err != nil {
		return err
	}
	if _, err = runQuiet("schtasks", "/Create", "/TN", TaskName, "/XML", path, "/F"); err != nil {
		return err
	}
	fmt.Fprint(out, i18n.F("Registered scheduled task %q (runs at logon as %s)\n", "已注册计划任务 %q（以 %s 身份在登录时运行）\n", TaskName, spec.UserID))
	return nil
}

// updateDefinition leaves the scheduled task alone: re-creating it could stop
// the running host, and the host reads its settings from service.json on
// every start.
func updateDefinition(*Config) error { return nil }

// pathHint has nothing to check: the supervisor starts the host with the
// user's own environment each time.
func pathHint(*Config, io.Writer) {}

func unregister(dryRun bool, out io.Writer) error {
	if dryRun {
		fmt.Fprintf(out, "[dry-run] schtasks /Delete /TN %q /F\n", TaskName)
		return nil
	}
	if !schedulerState().Installed {
		fmt.Fprint(out, i18n.F("Scheduled task %q is not registered\n", "计划任务 %q 未注册\n", TaskName))
		return nil
	}
	if _, err := runQuiet("schtasks", "/Delete", "/TN", TaskName, "/F"); err != nil {
		return err
	}
	fmt.Fprint(out, i18n.F("Deleted scheduled task %q\n", "已删除计划任务 %q\n", TaskName))
	return nil
}

func schedulerStart() error {
	_, err := runQuiet("schtasks", "/Run", "/TN", TaskName)
	return err
}

// schedulerStop hard-terminates the task; the supervisor's job object takes
// the host and its children down with it. It is idempotent: a task that is
// not running (or not registered) is left alone, and an /End error is ignored
// when the task is no longer running afterwards.
func schedulerStop() error {
	if taskState() != "Running" {
		return nil
	}
	_, err := runQuiet("schtasks", "/End", "/TN", TaskName)
	if err != nil && taskState() != "Running" {
		return nil
	}
	return err
}

// taskState returns the unlocalized Task Scheduler state (Ready, Running,
// Disabled, ...), or "" when the task is not registered.
func taskState() string {
	state, _, _ := queryTask()
	return state
}

// queryTask uses Get-ScheduledTask because its State values are not localized.
func queryTask() (state, lastRun, lastResult string) {
	out, err := runQuiet("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"$t = Get-ScheduledTask -TaskName '"+TaskName+"' -ErrorAction Stop; $i = $t | Get-ScheduledTaskInfo; \"$($t.State)|$($i.LastRunTime)|$($i.LastTaskResult)\"")
	if err != nil {
		return "", "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(out), "|", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	return parts[0], parts[1], parts[2]
}

func schedulerState() schedState {
	state, lastRun, lastResult := queryTask()
	if state == "" {
		return schedState{}
	}
	st := schedState{Installed: true, Enabled: state != "Disabled", Detail: "state=" + state}
	if lastRun != "" || lastResult != "" {
		st.Detail += fmt.Sprintf(" lastRun=%s lastResult=%s", lastRun, lastResult)
	}
	return st
}

func firewallHint(exposed []string, out io.Writer) {
	ports := map[string]bool{}
	for _, l := range exposed {
		if _, p, err := net.SplitHostPort(l); err == nil {
			ports[p] = true
		}
	}
	list := make([]string, 0, len(ports))
	for p := range ports {
		list = append(list, p)
	}
	sort.Strings(list)
	fmt.Fprintln(out, i18n.T("Windows Firewall may block LAN clients. Optional (run in an elevated shell; not executed):", "Windows 防火墙可能拦截局域网客户端。可选（在管理员终端中运行；此处不会执行）："))
	for _, p := range list {
		fmt.Fprintf(out, "  netsh advfirewall firewall add rule name=\"CC Remote\" dir=in action=allow protocol=TCP localport=%s profile=private\n", p)
	}
}

func logLine(path, format string, args ...any) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s [supervisor] %s\n", time.Now().Format("2006/01/02 15:04:05"), fmt.Sprintf(format, args...))
}

// maxConflictRetries is how many consecutive ExitConflict exits the
// supervisor tolerates (a previous host may still be shutting down).
const maxConflictRetries = 3

// logAppender writes each chunk to path, opening and closing it per write.
type logAppender string

func (p logAppender) Write(b []byte) (int, error) {
	if p == "" {
		return len(b), nil
	}
	f, err := os.OpenFile(string(p), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return len(b), nil // never block the host on a log failure
	}
	defer f.Close()
	_, _ = f.Write(b)
	return len(b), nil
}

// supervisedEnv is the service environment for the next host start. The
// scheduled task does not load the user's shell profile, so proxy settings
// and --env values come from <data-dir>\service.json, read on every start so
// "service config" changes apply when the host restarts itself.
func supervisedEnv(dataDir, logPath string) map[string]string {
	if dataDir == "" {
		return nil
	}
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		logLine(logPath, "read %s: %v; starting the host without the service environment", filepath.Join(dataDir, configName), err)
		return nil
	}
	if cfg == nil || len(cfg.Env) == 0 {
		return nil
	}
	logLine(logPath, "service environment: %s", EnvSummary(cfg.Env))
	return cfg.Env
}

// Supervise runs the console host hidden and restarts it after a failed exit.
// A clean exit (code 0, e.g. /api/admin/shutdown) ends the task.
func Supervise(args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	exe := filepath.Join(filepath.Dir(self), "cc-remote.exe")
	logPath, dataDir := "", ""
	for i, a := range args {
		if i+1 < len(args) {
			switch a {
			case "--log-file":
				logPath = args[i+1]
			case "--data-dir":
				dataDir = args[i+1]
			}
		}
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return err
	}
	backoff := 5 * time.Second
	conflicts := 0
	for {
		cmd := exec.Command(exe, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
		if env := supervisedEnv(dataDir, logPath); len(env) > 0 {
			cmd.Env = mergeEnviron(os.Environ(), env, true)
		}
		// Startup failures and Go runtime crashes printed before the host opens
		// its log file land in the same log. Each write opens and closes the
		// file so the host can still rotate it; WaitDelay bounds Wait if a
		// grandchild inherited the pipe.
		cmd.Stderr = logAppender(logPath)
		cmd.WaitDelay = 5 * time.Second
		started := time.Now()
		if err = cmd.Start(); err != nil {
			logLine(logPath, "start %s: %v", exe, err)
			return err
		}
		if h, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid)); e == nil {
			_ = windows.AssignProcessToJobObject(job, h)
			windows.CloseHandle(h)
		}
		err = cmd.Wait()
		if err == nil {
			logLine(logPath, "host exited cleanly")
			return nil
		}
		if time.Since(started) > time.Minute {
			backoff = 5 * time.Second
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == ExitRestart {
			logLine(logPath, "host restarting (e.g. to run an installed update)")
			backoff, conflicts = 5*time.Second, 0
			time.Sleep(time.Second)
			continue
		}
		if errors.As(err, &exitErr) && exitErr.ExitCode() == ExitConflict {
			conflicts++
			if conflicts >= maxConflictRetries {
				logLine(logPath, "host exited %d times because the data directory or listen port is held by another process (another cc-remote?); giving up. Stop the other host, then run: cc-remote service start", conflicts)
				return ExitError{Code: ExitConflict}
			}
		} else {
			conflicts = 0
		}
		logLine(logPath, "host exited: %v; restarting in %s", err, backoff)
		time.Sleep(backoff)
		backoff = min(backoff*2, time.Minute)
	}
}
