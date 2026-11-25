package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
)

// CLICandidate describes the release and updater available to the configured CLI.
// Ready means the tool's own updater resolves the target.
type CLICandidate struct {
	CurrentVersion, Version, Notes, NotesURL, Message string
	Ready, Available, Unavailable                     bool
}

type cliSpec struct {
	id, name, npmPackage, repo, tagPrefix, configEnv string
}

func cliFor(id string) (cliSpec, error) {
	switch id {
	case "claude":
		return cliSpec{id, "Claude Code", "@anthropic-ai/claude-code", "anthropics/claude-code", "v", "CLAUDE_CONFIG_DIR"}, nil
	case "codex":
		return cliSpec{id, "Codex", "@openai/codex", "openai/codex", "rust-v", "CODEX_HOME"}, nil
	default:
		return cliSpec{}, errors.New(i18n.T("Unknown CLI component", "未知的命令行组件"))
	}
}

func (s cliSpec) releaseURL(version string) string {
	base := "https://github.com/" + s.repo + "/releases"
	if version != "" {
		return base + "/tag/" + s.tagPrefix + version
	}
	return base
}

// CheckCLI checks the configured executable and its installation owner, without
// installing anything. npm releases are resolved using the host's npm registry.
func CheckCLI(ctx context.Context, id, executable, configDir string) (CLICandidate, error) {
	spec, err := cliFor(id)
	if err != nil {
		return CLICandidate{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	target, err := openCLITarget(ctx, spec, executable, configDir)
	if err != nil {
		return CLICandidate{}, err
	}
	defer target.close()
	result := CLICandidate{NotesURL: spec.releaseURL("")}
	if target.executable == "" {
		result.Unavailable = true
		result.Message = i18n.F("%s is not installed or is not on the host's PATH.", "%s 未安装，或未加入服务器的 PATH。", spec.name)
		return result, nil
	}
	result.CurrentVersion = target.version(ctx)
	switch {
	case target.selfUpdate:
		if result.CurrentVersion == "" {
			return result, versionReadError(spec)
		}
		if !target.canSelfUpdate(ctx, result.CurrentVersion) {
			result.Unavailable = true
			result.Message = originalInstallerMessage(spec)
			return result, nil
		}
		result.Ready = true
		result.Message = i18n.F("%s will check and update using its own updater.", "%s 将使用自带的更新程序检查并更新。", spec.name)
	case target.npm.path != "":
		if result.CurrentVersion == "" {
			return result, versionReadError(spec)
		}
		raw, err := target.run(ctx, target.npm, "view", spec.npmPackage+"@latest", "version", "--json", "--fetch-retries=0")
		if err != nil {
			return result, i18n.Errorf("Could not check %s in the host's npm registry.", "无法通过服务器的 npm 软件源检查 %s。", spec.name)
		}
		if json.Unmarshal(raw, &result.Version) != nil || !validCLIVersion(result.Version) {
			return result, i18n.Errorf("npm did not return a supported %s version.", "npm 未返回可识别的 %s 版本。", spec.name)
		}
		cmp, ok := compareVersions(result.Version, result.CurrentVersion)
		if !ok {
			return result, versionReadError(spec)
		}
		result.Available = cmp > 0
		result.NotesURL = spec.releaseURL(result.Version)
		if cmp >= 0 {
			result.Notes = spec.releaseNotes(ctx, result.Version)
		}
	default:
		result.Unavailable = true
		result.Message = i18n.F("Update %s using the package manager or installer that installed this executable.", "请使用此 %s 可执行文件原有的包管理器或安装程序更新。", spec.name)
	}
	return result, nil
}

// InstallCLI updates only the detected owner of the same configured executable.
// The returned version is read from that executable after installation.
func InstallCLI(ctx context.Context, id, executable, configDir string, candidate CLICandidate) (string, error) {
	spec, err := cliFor(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	target, err := openCLITarget(ctx, spec, executable, configDir)
	if err != nil {
		return "", err
	}
	defer target.close()
	current := target.version(ctx)
	if current == "" {
		return "", versionReadError(spec)
	}
	if current != candidate.CurrentVersion {
		return current, i18n.Errorf("%s changed since the last check. Check for updates again.", "%s 自上次检查后已变更，请重新检查更新。", spec.name)
	}
	if candidate.Unavailable {
		return current, errors.New(originalInstallerMessage(spec))
	}
	var command cliCommand
	var args []string
	switch {
	case candidate.Ready && !candidate.Available && candidate.Version == "" && target.selfUpdate:
		if !target.canSelfUpdate(ctx, current) {
			return current, errors.New(originalInstallerMessage(spec))
		}
		command, args = target.provider, []string{"update"}
	case candidate.Available && !candidate.Ready && target.npm.path != "" && validCLIVersion(candidate.Version):
		cmp, ok := compareVersions(candidate.Version, current)
		if !ok || cmp <= 0 {
			return current, i18n.Errorf("The requested %s version is no longer an update. Check for updates again.", "请求的 %s 版本已不是可用更新，请重新检查。", spec.name)
		}
		command = target.npm
		args = []string{"install", "--global", "--prefix", target.npmPrefix, "--no-audit", "--no-fund", spec.npmPackage + "@" + candidate.Version}
	default:
		return current, i18n.Errorf("Could not confirm the update method for %s. Check for updates again.", "无法确认 %s 的更新方式，请重新检查更新。", spec.name)
	}
	if _, err := target.run(ctx, command, args...); err != nil {
		return current, i18n.Errorf("The %s updater failed. Check that the host can reach the release source and has permission to update this installation.", "%s 更新失败，请确认服务器可以访问发布源，并有权限更新此安装。", spec.name)
	}
	// npm can replace its launcher or switch from a script to a native binary.
	refreshed, err := openCLITarget(ctx, spec, executable, configDir)
	if err != nil {
		return current, err
	}
	defer refreshed.close()
	got := refreshed.version(ctx)
	if got == "" {
		return current, versionReadError(spec)
	}
	if candidate.Available && got != candidate.Version {
		return got, i18n.Errorf("The configured %s executable does not report the requested version after the update.", "更新后配置的 %s 可执行文件未报告所请求的版本。", spec.name)
	}
	return got, nil
}

func versionReadError(s cliSpec) error {
	return i18n.Errorf("Could not read the version of the configured %s executable.", "无法读取配置的 %s 可执行文件版本。", s.name)
}

func originalInstallerMessage(s cliSpec) string {
	return i18n.F("This standalone %s version needs to be updated with its original installer.", "此独立安装的 %s 版本需使用原安装程序更新。", s.name)
}

// Only use concrete versions as npm arguments. Explicit prerelease identifiers
// also cover installed beta versions when comparing them to the latest tag.
var cliVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

func validCLIVersion(version string) bool {
	if len(version) > 128 || !cliVersionPattern.MatchString(version) {
		return false
	}
	parsed, ok := parseSemver(version)
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(parsed.pre, ".") {
		if len(part) > 1 && part[0] == '0' && strings.Trim(part, "0123456789") == "" {
			return false
		}
	}
	return true
}

func cliVersion(id, stdout string) string {
	fields := strings.Fields(stdout)
	if id == "codex" && len(fields) >= 2 && fields[0] == "codex-cli" {
		fields = fields[1:]
	}
	if len(fields) > 0 && validCLIVersion(fields[0]) {
		return fields[0]
	}
	return ""
}

type cliCommand struct {
	path string
	args []string
}

type cliTarget struct {
	spec            cliSpec
	executable, dir string
	env             []string
	provider, npm   cliCommand
	npmPrefix       string
	selfUpdate      bool
	home            string
}

func openCLITarget(ctx context.Context, spec cliSpec, executable, configDir string) (cliTarget, error) {
	dir, err := os.MkdirTemp("", "cc-remote-cli-update-")
	if err != nil {
		return cliTarget{}, i18n.Errorf("Could not create a working directory for the updater.", "无法为更新程序创建工作目录。")
	}
	target := cliTarget{spec: spec, dir: dir, env: cliEnvironment(spec.configEnv, configDir)}
	if executable == "" {
		executable = spec.id
	}
	path, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(path) {
		return target, nil
	}
	target.executable = path
	target.provider = cliCommand{path: path}
	if windowsShim(path) {
		target.provider = cliCommand{}
		if spec.id == "codex" {
			if cmd, err := codex.Command(path); err == nil {
				target.provider = cliCommand{path: cmd.Path, args: cmd.Args[1:]}
			}
		}
	}
	target.home, _ = os.UserHomeDir()
	switch spec.id {
	case "claude":
		target.selfUpdate = nativeClaude(path, target.home, runtime.GOOS)
	case "codex":
		codexHome, bin := codexStandalonePaths(target.home, configDir)
		target.selfUpdate = nativeCodex(path, codexHome, bin, runtime.GOOS)
	}
	if !target.selfUpdate {
		// npm ownership is checked both before the version lookup and before
		// installation. A different npm prefix may point to a second copy.
		probeCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
		defer cancel()
		target.detectNPM(probeCtx)
	}
	return target, nil
}

func (t cliTarget) close() { _ = os.RemoveAll(t.dir) }

func (t cliTarget) run(ctx context.Context, cmdSpec cliCommand, args ...string) ([]byte, error) {
	if !filepath.IsAbs(cmdSpec.path) {
		return nil, errors.New("missing executable")
	}
	cmd := exec.CommandContext(ctx, cmdSpec.path, append(append([]string(nil), cmdSpec.args...), args...)...)
	cmd.Dir, cmd.Env = t.dir, t.env
	cmd.WaitDelay = 3 * time.Second
	// Tools can print configuration and registry errors. Keep only bounded
	// stdout for structured parsing and discard all tool output on failure.
	var out cliOutput
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.buf.Bytes(), nil
}

type cliOutput struct{ buf bytes.Buffer }

func (b *cliOutput) Write(p []byte) (int, error) {
	const limit = 64 << 10
	if remaining := limit - b.buf.Len(); remaining > 0 {
		_, _ = b.buf.Write(p[:min(len(p), remaining)])
	}
	return len(p), nil
}

func (t cliTarget) version(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := t.run(ctx, t.provider, "--version")
	if err != nil {
		return ""
	}
	return cliVersion(t.spec.id, string(b))
}

func (t cliTarget) canSelfUpdate(ctx context.Context, version string) bool {
	if t.spec.id == "claude" {
		if runtime.GOOS != "windows" {
			return true
		}
		// The native Windows launcher is a copy of the versioned binary.
		// A custom launcher can live at the same path without being managed.
		return windowsClaudeVersionMatches(t.executable, t.home, version)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	b, err := t.run(ctx, t.provider, "update", "--help")
	return err == nil && bytes.Contains(b, []byte("Usage: codex update"))
}

func windowsClaudeVersionMatches(executable, home, version string) bool {
	launcher, err := os.Lstat(executable)
	bin, binErr := os.Lstat(filepath.Join(home, ".local", "share", "claude", "versions", version))
	return err == nil && binErr == nil && launcher.Mode().IsRegular() && bin.Mode().IsRegular() &&
		launcher.Size() == bin.Size()
}

func cliEnvironment(configEnv, configDir string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		switch {
		case strings.HasPrefix(key, "CC_REMOTE_"), key == "CLAUDECODE", configDir != "" && key == configEnv,
			key == "CODEX_NON_INTERACTIVE", key == "CODEX_RELEASE", strings.HasPrefix(key, "CODEX_INSTALL_DAEMON_"),
			key == "CODEX_INSTALL_IF_CURRENT", key == "CODEX_INSTALL_PREVIOUS_RELEASE", key == "CODEX_INSTALL_DEFER_SELECTION":
			continue
		}
		env = append(env, entry)
	}
	if configDir != "" {
		env = append(env, configEnv+"="+configDir)
	}
	return append(env, "CODEX_NON_INTERACTIVE=1")
}

func nativeClaude(executable, home, goos string) bool {
	if !filepath.IsAbs(home) {
		return false
	}
	if goos == "windows" {
		return samePath(executable, filepath.Join(home, ".local", "bin", "claude.exe"), goos) &&
			regularFile(executable)
	}
	if !samePath(executable, filepath.Join(home, ".local", "bin", "claude"), goos) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(executable)
	root, rootErr := filepath.EvalSymlinks(filepath.Join(home, ".local", "share", "claude", "versions"))
	return err == nil && rootErr == nil && samePath(filepath.Dir(resolved), root, goos) &&
		validCLIVersion(filepath.Base(resolved)) && regularFile(resolved)
}

func codexStandalonePaths(home, configDir string) (string, string) {
	if configDir == "" {
		configDir = os.Getenv("CODEX_HOME")
	}
	if configDir == "" && filepath.IsAbs(home) {
		configDir = filepath.Join(home, ".codex")
	}
	bin := os.Getenv("CODEX_INSTALL_DIR")
	if bin == "" {
		if runtime.GOOS == "windows" {
			if local := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(local) {
				bin = filepath.Join(local, "Programs", "OpenAI", "Codex", "bin")
			}
		} else if filepath.IsAbs(home) {
			bin = filepath.Join(home, ".local", "bin")
		}
	}
	return configDir, bin
}

func nativeCodex(executable, codexHome, bin, goos string) bool {
	if !filepath.IsAbs(codexHome) || !filepath.IsAbs(bin) {
		return false
	}
	name := "codex"
	if goos == "windows" {
		name = "codex.exe"
	}
	if !samePath(executable, filepath.Join(bin, name), goos) {
		return false
	}
	owner := filepath.Join(codexHome, "packages", "standalone")
	current := filepath.Join(owner, "current")
	release := linkedPath(current)
	if release == "" || !samePath(filepath.Dir(release), filepath.Join(owner, "releases"), goos) || !regularFile(executable) {
		return false
	}
	// The official Windows installer uses two nested junctions.
	// EvalSymlinks does not reliably follow such a path on Windows.
	if goos == "windows" {
		got := linkedPath(bin)
		return got != "" && (samePath(got, filepath.Join(current, "bin"), goos) || samePath(got, current, goos))
	}
	got := linkedPath(executable)
	return got != "" && (samePath(got, filepath.Join(current, "bin", name), goos) || samePath(got, filepath.Join(current, name), goos))
}

func linkedPath(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		return filepath.Join(filepath.Dir(path), target)
	}
	return target
}

func samePath(a, b, goos string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func regularFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

func windowsShim(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat", ".ps1":
		return true
	default:
		return false
	}
}

func nodeCommand(dir, entry string) cliCommand {
	node := filepath.Join(dir, "node.exe")
	if !regularFile(node) {
		node, _ = exec.LookPath("node")
	}
	if filepath.IsAbs(node) && regularFile(entry) {
		return cliCommand{path: node, args: []string{entry}}
	}
	return cliCommand{}
}

func (t *cliTarget) detectNPM(ctx context.Context) {
	path, _ := exec.LookPath("npm")
	npm := cliCommand{path: path}
	if windowsShim(path) {
		dir := filepath.Dir(path)
		npm = nodeCommand(dir, filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js"))
	}
	prefixRaw, err := t.run(ctx, npm, "prefix", "--global")
	if err != nil {
		return
	}
	prefix := strings.TrimSpace(string(prefixRaw))
	if !npmLauncher(t.executable, prefix, t.spec.id, runtime.GOOS) {
		return
	}
	rootRaw, err := t.run(ctx, npm, "root", "--global")
	if err != nil {
		return
	}
	root := strings.TrimSpace(string(rootRaw))
	entry := npmPackageEntry(root, t.spec)
	if entry == "" {
		return
	}
	if runtime.GOOS == "windows" {
		// npm generates script launchers on Windows. Ensure this shim names
		// the verified package entry, then execute the entry without a shell.
		if !npmShimOwns(t.executable, entry, root, t.spec) {
			return
		}
		switch strings.ToLower(filepath.Ext(entry)) {
		case ".exe":
			t.provider = cliCommand{path: entry}
		case ".js", ".cjs", ".mjs":
			t.provider = nodeCommand(filepath.Dir(t.executable), entry)
		default:
			return
		}
		if t.provider.path == "" {
			return
		}
	} else {
		resolved, err := filepath.EvalSymlinks(t.executable)
		if err != nil || !samePath(resolved, entry, runtime.GOOS) {
			return
		}
	}
	t.npm, t.npmPrefix = npm, prefix
}

func npmLauncher(executable, prefix, name, goos string) bool {
	if !filepath.IsAbs(prefix) {
		return false
	}
	if goos != "windows" {
		return samePath(executable, filepath.Join(prefix, "bin", name), goos)
	}
	for _, ext := range []string{".cmd", ".ps1", ""} {
		if samePath(executable, filepath.Join(prefix, name+ext), goos) {
			return true
		}
	}
	return false
}

func npmPackageEntry(root string, spec cliSpec) string {
	if !filepath.IsAbs(root) {
		return ""
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	pkg := filepath.Join(root, filepath.FromSlash(spec.npmPackage))
	resolved, err := filepath.EvalSymlinks(pkg)
	if err != nil || !samePath(resolved, pkg, runtime.GOOS) {
		// A global npm link is a development checkout. npm update would
		// replace the link, leaving the configured installation behind.
		return ""
	}
	b, err := readCLIMetadata(filepath.Join(pkg, "package.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Name string          `json:"name"`
		Bin  json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(b, &manifest) != nil || manifest.Name != spec.npmPackage {
		return ""
	}
	var bin string
	var bins map[string]string
	if json.Unmarshal(manifest.Bin, &bins) == nil {
		bin = bins[spec.id]
	} else {
		_ = json.Unmarshal(manifest.Bin, &bin)
	}
	if bin == "" || filepath.IsAbs(bin) {
		return ""
	}
	entry, err := filepath.EvalSymlinks(filepath.Join(pkg, filepath.FromSlash(bin)))
	rel, relErr := filepath.Rel(pkg, entry)
	if err != nil || relErr != nil || !filepath.IsLocal(rel) || rel == "." || !regularFile(entry) {
		return ""
	}
	return entry
}

func npmShimOwns(shim, entry, root string, spec cliSpec) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil || !samePath(filepath.Join(filepath.Dir(shim), "node_modules"), root, "windows") {
		return false
	}
	reference, err := filepath.Rel(filepath.Join(resolvedRoot, filepath.FromSlash(spec.npmPackage)), entry)
	if err != nil || !filepath.IsLocal(reference) {
		return false
	}
	body, err := readCLIMetadata(shim)
	if err != nil {
		return false
	}
	want := "node_modules/" + spec.npmPackage + "/" + filepath.ToSlash(reference)
	have := strings.ToLower(strings.ReplaceAll(string(body), "\\", "/"))
	return strings.Contains(have, strings.ToLower(want)+"\"")
}

func readCLIMetadata(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 64<<10))
}

func (s cliSpec) releaseNotes(ctx context.Context, version string) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	src := &apiSource{base: "https://api.github.com", repo: s.repo, client: newHTTPClient()}
	release, err := src.release(ctx, s.tagPrefix+version)
	if err != nil {
		return ""
	}
	const limit = 32 << 10
	notes := strings.TrimSpace(release.Notes)
	if len(notes) <= limit {
		return notes
	}
	end := limit
	for !utf8.RuneStart(notes[end]) {
		end--
	}
	return notes[:end] + "…"
}
