package server

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"cc-remote/internal/i18n"
)

const maxSkillBytes = 256 << 10
const maxSkillFiles = 256

var skillName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*(?::[a-zA-Z0-9][a-zA-Z0-9_-]*)*$`)

type skillFile struct {
	name, path, base, description string
}

// Resolve within the configured root. A root may itself be a symlink, but a
// nested link cannot expose files outside it to remote command discovery.
func skillPath(root, path string) (string, bool) {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return realPath, true
}

// readSkill returns the body and the frontmatter description ("" when none).
func readSkill(l i18n.Lang, path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", "", l.Errorf("The skill file is not a regular file", "技能文件不是普通文件")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSkillBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(b) > maxSkillBytes {
		return "", "", l.Errorf("The skill file is larger than 256 KiB", "技能文件超过 256 KiB")
	}
	text := strings.TrimPrefix(string(b), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	description := ""
	if text == "---" || strings.HasPrefix(text, "---\n") {
		lines := strings.Split(text, "\n")
		end := -1
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
			if value, ok := strings.CutPrefix(lines[i], "description:"); ok {
				value = strings.TrimSpace(value)
				if strings.HasPrefix(value, "\"") {
					if v, e := strconv.Unquote(value); e == nil {
						value = v
					}
				} else if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
					value = strings.ReplaceAll(strings.Trim(value, "'"), "''", "'")
				}
				if value != "" && value != ">" && value != "|" && value != ">-" && value != "|-" {
					description = value
				}
			}
		}
		if end < 0 {
			return "", "", l.Errorf("The skill's frontmatter is not closed", "技能 frontmatter 未闭合")
		}
		text = strings.Join(lines[end+1:], "\n")
	}
	return strings.TrimSpace(text), description, nil
}

func (s *Server) discoveredSkills(cwd string) map[string]skillFile {
	found := map[string]skillFile{}
	count := 0
	// Inspect higher-priority roots first so the global file cap cannot let user
	// files hide project commands.
	for _, source := range []struct {
		root   string
		legacy bool
	}{
		{filepath.Join(cwd, ".claude", "skills"), false},
		{filepath.Join(cwd, ".claude", "commands"), true},
		{filepath.Join(s.cfg.ClaudeConfigDir, "skills"), false},
		{filepath.Join(s.cfg.ClaudeConfigDir, "commands"), true},
	} {
		root, err := filepath.EvalSymlinks(source.root)
		if err != nil {
			continue
		}
		add := func(name, path string) {
			if count >= maxSkillFiles || !skillName.MatchString(name) {
				return
			}
			if _, exists := found[name]; exists {
				return
			}
			resolved, ok := skillPath(root, path)
			if !ok {
				return
			}
			_, description, err := readSkill(i18n.EN, resolved)
			if err != nil {
				return
			}
			count++
			found[name] = skillFile{name, resolved, filepath.Dir(resolved), description}
		}
		if source.legacy {
			_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				rel, _ := filepath.Rel(root, path)
				if entry.IsDir() {
					if count >= maxSkillFiles || strings.Count(rel, string(filepath.Separator)) >= 8 {
						return filepath.SkipDir
					}
					return nil
				}
				if strings.HasSuffix(entry.Name(), ".md") {
					add(strings.ReplaceAll(strings.TrimSuffix(rel, ".md"), string(filepath.Separator), ":"), path)
				}
				return nil
			})
		} else {
			entries, _ := os.ReadDir(root)
			for _, entry := range entries {
				if count >= maxSkillFiles {
					break
				}
				add(entry.Name(), filepath.Join(root, entry.Name(), "SKILL.md"))
			}
		}
	}
	return found
}

func (s *Server) skillCommands(l i18n.Lang, cwd string) []webCommand {
	files := s.discoveredSkills(cwd)
	commands := make([]webCommand, 0, len(files))
	for _, f := range files {
		if reservedCommand(f.name) {
			continue
		}
		commands = append(commands, webCommand{Name: f.name, Description: skillDescription(l, f)})
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return commands
}

func (s *Server) skillPrompt(l i18n.Lang, cwd, name, args string) (string, bool, error) {
	if !skillName.MatchString(name) || reservedCommand(name) {
		return "", false, nil
	}
	f, ok := s.discoveredSkills(cwd)[name]
	if !ok {
		return "", false, nil
	}
	body, _, err := readSkill(l, f.path)
	if err != nil {
		return "", true, err
	}
	// Dynamic shell preprocessing belongs to native Claude Code. Never silently
	// convert its shell syntax into an apparently successful static invocation.
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 4096), maxSkillBytes+1)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "!`") || strings.HasPrefix(line, "! ") || strings.HasPrefix(line, "!\t") {
			return "", true, l.Errorf("This instruction uses dynamic shell preprocessing; run it in the native Claude terminal", "此指令含动态 shell 预处理，请在原生 Claude 终端执行")
		}
	}
	if err = scanner.Err(); err != nil {
		return "", true, err
	}
	if strings.TrimSpace(body) == "" {
		return "", true, l.Errorf("The skill body is empty", "技能正文为空")
	}
	if regexp.MustCompile(`\$ARGUMENTS\[|\$[0-9]`).MatchString(body) {
		return "", true, l.Errorf("This instruction uses positional arguments; run it in the native Claude terminal (the web only supports $ARGUMENTS)", "此指令使用位置参数，请在原生 Claude 终端执行；网页仅支持 $ARGUMENTS")
	}
	const maxExpanded = 1 << 20
	count := strings.Count(body, "$ARGUMENTS")
	if len(args) > maxExpanded-len(body) || (count > 0 && len(args) > (maxExpanded-len(body))/count) {
		return "", true, l.Errorf("The expanded instruction is larger than 1 MiB", "展开后的指令超过 1 MiB")
	}
	hasArgs := strings.Contains(body, "$ARGUMENTS")
	body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	if !hasArgs && strings.TrimSpace(args) != "" {
		body += "\n\nUser arguments: " + args
	}
	return fmt.Sprintf("User invoked custom Claude instruction /%s. Skill base directory: %q. Resolve relative skill resources against that directory. This is a static prompt expansion; native skill hooks, frontmatter execution options, and shell preprocessing are not executed by this web bridge.\n\n%s", name, f.base, body), true, nil
}

// skillDescription is a skill's frontmatter description, else a generic one.
func skillDescription(l i18n.Lang, f skillFile) string {
	if f.description != "" {
		return f.description
	}
	return l.T("Custom Claude instruction", "自定义 Claude 指令")
}
