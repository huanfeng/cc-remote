package server

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"maps"
	"net/http"
	"os"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
	"cc-remote/internal/terminal"
)

type codexTerminalRequest struct {
	Title, Cwd, Resume, Prompt string
	Args                       []string
	ProfileID                  string
}

func (s *Server) createCodexTerminal(w http.ResponseWriter, r *http.Request, req codexTerminalRequest) {
	l := s.lang(r)
	req.Title = strings.TrimSpace(req.Title)
	if len(req.Title) > 300 || len(req.Prompt) > 16<<10 || strings.ContainsRune(req.Prompt, 0) {
		fail(w, 400, l.T("Invalid Codex terminal title or prompt", "Codex 终端标题或提示词无效"))
		return
	}
	args, err := codexTerminalArgs(req.Args)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	req.Resume = strings.ToLower(strings.TrimSpace(req.Resume))
	if req.Resume != "" && !channelUUID.MatchString(req.Resume) {
		fail(w, 400, l.T("Invalid Codex thread ID", "无效的 Codex 对话 ID"))
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		fail(w, 503, l.T("The host is shutting down", "宿主正在关闭"))
		return
	}
	var x store.Session
	var modelProvider string
	if req.Resume != "" {
		x, err = s.store.FindNative("codex", req.Resume)
		switch {
		case err == nil:
			if x.Mode != "terminal" {
				fail(w, 409, l.F("This Codex thread is already a managed session named %q", "这个 Codex 对话已是名为 %q 的托管会话", x.Title))
				return
			}
			if ts := s.terminals[x.ID]; ts != nil {
				if ts.closing || isDone(ts.runner.Done()) {
					fail(w, 409, l.T("The terminal is closing; try again shortly", "终端正在关闭，请稍后重试"))
					return
				}
				// A native /resume or /fork may have changed the thread.
				// Only the CC Remote session ID still identifies this terminal.
				fail(w, 409, l.F("A terminal was launched with this thread. Check it with ccr session attach %s", "这个对话已用于启动一个终端。请用 ccr session attach %s 接入并确认当前对话", x.ID))
				return
			}
		case !errors.Is(err, sql.ErrNoRows):
			fail(w, 500, l.T("Could not read sessions", "无法读取会话"))
			return
		}
		native, _, err := codex.NativeHistory(s.cfg.CodexConfigDir, req.Resume)
		if err != nil {
			fail(w, 404, l.T("Codex thread not found", "找不到这个 Codex 对话"))
			return
		}
		req.Cwd = native.Cwd
		modelProvider = native.ModelProvider
		if req.Title == "" {
			req.Title = native.Title
		}
	}
	cwd, err := validateCwd(l, req.Cwd)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if req.Resume != "" {
		// An unbound TUI may have opened this native thread from its picker.
		project, _ := os.Stat(cwd)
		for _, ts := range s.terminals {
			if !ts.codex || isDone(ts.runner.Done()) {
				continue
			}
			dir, _ := os.Stat(ts.cwd)
			if cwd == ts.cwd || project != nil && dir != nil && os.SameFile(project, dir) {
				fail(w, 409, l.F("A Codex terminal is already open in this project. Check it with ccr session attach %s", "这个项目已有 Codex 终端。请用 ccr session attach %s 接入并确认当前对话", ts.id))
				return
			}
		}
	}
	created := x.ID == ""
	x, err = s.codexTerminalProfile(x, req, modelProvider, args)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	var options claude.Options
	if err := s.profileOptions(x, &options); err != nil {
		fail(w, 400, err.Error())
		return
	}
	configArgs, err := codexTerminalConfigArgs(options, args, x.ProfileID)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if created {
		if req.Title == "" {
			req.Title = folderTitle(cwd)
			if req.Title == "" {
				req.Title = l.T("New session", "新会话")
			}
		}
		nativeID := req.Resume
		if nativeID == "" {
			nativeID = store.ID() // internal placeholder until the native ID can be verified
		}
		x.Title, x.Cwd, x.ClaudeSessionID, x.Resume = req.Title, cwd, nativeID, req.Resume != ""
		x, err = s.store.CreateTerminal(x)
		if err != nil {
			fail(w, 500, l.T("Could not create the session", "无法创建会话"))
			return
		}
	}
	env := claude.ProfileEnvironment(codex.Environment(s.cfg.CodexConfigDir), options.ProfileEnv)
	if err := s.startCodexTerminalLocked(x.ID, cwd, req.Resume, req.Prompt, append(configArgs, args...), env); err != nil {
		if created {
			if deleteErr := s.store.Delete(x.ID); deleteErr != nil {
				log.Printf("remove failed Codex terminal %s: %v", x.ID, deleteErr)
			}
		}
		fail(w, 500, l.T("Could not start Codex: ", "无法启动 Codex：")+err.Error())
		return
	}
	s.decorate(&x)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(w, status, x)
}

const codexTerminalSupplierPrefix = "ccr_supplier_"

// Codex writes its model_provider when it creates a thread. Use it to recover
// the supplier even when CCR has never seen the native thread ID before.
func codexTerminalSupplierAlias(id string) string {
	return codexTerminalSupplierPrefix + base64.RawURLEncoding.EncodeToString([]byte(id))
}

func (s *Server) codexTerminalProfile(x store.Session, req codexTerminalRequest, modelProvider string, args []string) (store.Session, error) {
	wanted := req.ProfileID
	var recorded string
	nativeOptions := false
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "-p", "--profile", "--oss", "--local-provider":
			nativeOptions = true
		}
	}
	if req.Resume != "" {
		bound := x.ProfileID
		if encoded, ok := strings.CutPrefix(modelProvider, codexTerminalSupplierPrefix); ok {
			id, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
			if err != nil || len(id) == 0 {
				return x, i18n.Errorf("Invalid supplier in this Codex thread", "这个 Codex 对话的供应商标识无效")
			}
			recorded = string(id)
			bound = recorded
			if x.ID != "" && x.ProfileID != bound {
				return x, i18n.Errorf("The saved supplier does not match this Codex thread", "保存的供应商与这个 Codex 对话不一致")
			}
		}
		if x.ID != "" || bound != "" {
			if bound == "" {
				bound = "@native"
			}
			if wanted != "" && !profileIDMatches(bound, wanted) {
				return x, i18n.Errorf("Resume requires this Codex thread's original supplier", "恢复 Codex 对话需要沿用原来的供应商")
			}
			wanted = bound
		} else if wanted == "" {
			wanted = "@native" // older native threads must not inherit a new CCR default
		}
	}
	if nativeOptions {
		if wanted != "" && wanted != "@native" {
			return x, i18n.Errorf("Native Codex provider options require --supplier @native", "Codex 原生供应商参数需要使用 --supplier @native")
		}
		wanted = "@native"
	}
	if x.ID == "" {
		profile, err := s.selectProfile("codex", wanted)
		if err != nil {
			return x, err
		}
		if recorded != "" && profile.ID != recorded {
			return x, i18n.Errorf("Invalid supplier in this Codex thread", "这个 Codex 对话的供应商标识无效")
		}
		x.Provider, x.ProfileID, x.ProfileJSON, x.ProfileName = "codex", profile.ID, profile.snapshot(), profile.Name
	}
	return x, nil
}

func codexTerminalConfigArgs(options claude.Options, args []string, profileID string) ([]string, error) {
	if profileID == "" {
		return nil, nil
	}
	config := maps.Clone(options.ConfigOverrides)
	providers := config["model_providers"].(map[string]any) // checked by profileOptions
	alias := codexTerminalSupplierAlias(profileID)
	config["model_provider"] = alias
	config["model_providers"] = map[string]any{alias: providers[options.ModelProvider]}
	for _, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		if name == "-m" || name == "--model" {
			delete(config, "model")
			break
		}
	}
	return codex.ConfigArgs(config)
}

// codexTerminalArgs accepts only options that keep this process an interactive
// Codex in the host-selected directory and on the host-selected thread.
func codexTerminalArgs(args []string) ([]string, error) {
	size := 0
	for _, arg := range args {
		size += len(arg)
		if strings.ContainsRune(arg, 0) {
			return nil, i18n.Errorf("Invalid Codex argument", "Codex 参数无效")
		}
	}
	if len(args) > 100 || size > 16<<10 {
		return nil, i18n.Errorf("Too many Codex arguments", "Codex 参数过多")
	}
	var options []string
	for len(args) > 0 {
		raw := args[0]
		args = args[1:]
		name, value, inline := strings.Cut(raw, "=")
		switch name {
		case "--no-alt-screen", "--search", "--oss", "--strict-config":
			if inline {
				return nil, i18n.Errorf("Codex option %s takes no value", "Codex 参数 %s 不需要值", name)
			}
			options = append(options, name)
		case "-m", "--model", "-p", "--profile", "-s", "--sandbox", "-a", "--ask-for-approval", "--local-provider":
			if !inline && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
				value, args = args[0], args[1:]
			}
			if value == "" {
				return nil, i18n.Errorf("Codex option %s requires a value", "Codex 参数 %s 需要值", name)
			}
			if name == "-s" || name == "--sandbox" {
				switch value {
				case "read-only", "workspace-write", "danger-full-access":
				default:
					return nil, i18n.Errorf("Invalid Codex sandbox mode", "无效的 Codex 沙箱模式")
				}
			}
			if name == "-a" || name == "--ask-for-approval" {
				switch value {
				case "on-request", "never":
				default:
					return nil, i18n.Errorf("Invalid Codex approval mode", "无效的 Codex 审批模式")
				}
			}
			options = append(options, name+"="+value)
		default:
			return nil, i18n.Errorf("A terminal session cannot pass %q to Codex", "终端会话不能把 %q 传给 Codex", raw)
		}
	}
	return options, nil
}

func (s *Server) startCodexTerminalLocked(id, cwd, nativeID, prompt string, options, env []string) error {
	if s.switching[id] {
		return fmt.Errorf("正在切换配置")
	}
	var args []string
	if nativeID != "" {
		args = append(args, "resume")
	}
	args = append(args, "--no-daemon")
	args = append(args, options...)
	if nativeID != "" {
		args = append(args, "--", nativeID)
	} else if prompt != "" {
		args = append(args, "--")
	}
	if prompt != "" {
		args = append(args, prompt)
	}
	cmd, err := codex.Command(s.cfg.CodexExecutable, args...)
	if err != nil {
		return err
	}
	runner, err := terminal.Start(terminal.Options{Executable: cmd.Path, Args: cmd.Args[1:], Dir: cwd, Env: env, ReplayAlternateScreen: true})
	if err != nil {
		return err
	}
	ts := &terminalSession{id: id, codex: true, runner: runner, cleaned: make(chan struct{}), cwd: cwd, started: time.Now()}
	s.terminals[id] = ts
	s.closers.Add(1)
	go func() {
		defer s.closers.Done()
		<-runner.Done()
		defer close(ts.cleaned)
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.terminals, id)
		if !ts.closing && runner.Err() != nil {
			_, _ = s.store.Append(id, "error", i18n.F("Codex exited: %v", "Codex 已退出：%v", runner.Err()), nil)
		}
	}()
	return nil
}
