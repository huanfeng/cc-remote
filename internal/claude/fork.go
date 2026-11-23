package claude

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// ForkNativeSession materializes an independently resumable Claude transcript.
// A non-empty lastMessageID is the inclusive native boundary.
func ForkNativeSession(ctx context.Context, o Options, lastMessageID string) (string, error) {
	if !uuidPattern.MatchString(o.SessionID) {
		return "", errors.New("invalid source Claude session ID")
	}
	if lastMessageID != "" && !uuidPattern.MatchString(lastMessageID) {
		return "", errors.New("invalid Claude fork message ID")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	projects, err := projectsDir(o.ConfigDir)
	if err != nil {
		return "", err
	}
	projects, err = filepath.Abs(projects)
	if err != nil {
		return "", err
	}
	configDir := filepath.Dir(projects)
	source, metadata, err := openNativeSearch(configDir, o.SessionID)
	if err != nil {
		return "", fmt.Errorf("read source Claude session: %w", err)
	}
	defer source.Close()
	cwd := o.Cwd
	if cwd == "" {
		cwd = metadata.Cwd
	}
	if !sameForkDirectory(cwd, metadata.Cwd) {
		return "", errors.New("Claude fork source has a different working directory")
	}
	if lastMessageID != "" {
		if err := verifyForkHistory(ctx, source, lastMessageID, false); err != nil {
			return "", fmt.Errorf("verify source Claude fork message: %w", err)
		}
	}

	childID := forkSessionID()
	executable := o.Executable
	if executable == "" {
		executable = "claude"
	}
	args := []string{"--bare", "-p", "--verbose", "--output-format", "stream-json",
		"--resume", source.Name(), "--fork-session", "--session-id", childID}
	if lastMessageID != "" {
		args = append(args, "--resume-session-at", lastMessageID)
	}
	// Claude writes a resumed fork lazily. /cost is a built-in local command
	// that materializes it without a model call. Its generated transcript
	// records are already filtered out of the conversation history.
	args = append(args, "--", "/cost")
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir, cmd.Env = cwd, forkEnvironment(os.Environ(), configDir)
	configureProcess(cmd)
	cmd.Cancel = func() error {
		killProcessTree(cmd)
		return nil
	}
	cmd.WaitDelay = time.Second
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("fork Claude session: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	root, err := os.OpenRoot(projects)
	if err != nil {
		return "", err
	}
	defer root.Close()
	child, childMetadata, err := openNativeSearchFile(root, projects, filepath.Join(filepath.Dir(source.Name()), childID+".jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		// If Claude recalculated the project key, look for the child in its
		// actual transcript directory.
		child, childMetadata, err = openNativeSearch(configDir, childID)
	}
	if err != nil {
		return "", fmt.Errorf("open forked Claude session: %w", err)
	}
	defer child.Close()
	if !sameForkDirectory(cwd, childMetadata.Cwd) {
		return "", errors.New("forked Claude session has a different working directory")
	}
	if err := verifyForkHistory(ctx, child, lastMessageID, true); err != nil {
		return "", fmt.Errorf("verify forked Claude session: %w", err)
	}
	return childID, nil
}

func sameForkDirectory(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	x, xErr := os.Stat(left)
	y, yErr := os.Stat(right)
	return xErr == nil && yErr == nil && x.IsDir() && y.IsDir() && os.SameFile(x, y)
}

func forkSessionID() string {
	var id [16]byte
	_, _ = rand.Read(id[:])
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

func forkEnvironment(env []string, configDir string) []string {
	env = slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		return strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_") ||
			strings.HasPrefix(key, "CC_REMOTE_") || key == "CLAUDECODE" ||
			key == "HTTP_PROXY" || key == "HTTPS_PROXY" || key == "ALL_PROXY" || key == "NO_PROXY"
	})
	// Keep legacy releases from using an inherited supplier or performing a
	// real request if they no longer recognize the built-in local command.
	return append(env, "CLAUDE_CONFIG_DIR="+configDir, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:1", "ANTHROPIC_API_KEY=cc-remote-offline-fork", "NO_PROXY=*")
}

func verifyForkHistory(ctx context.Context, f *os.File, anchor string, child bool) error {
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	foundBoundary, hasConversation := false, false
	var metadata NativeSession
	for scan.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var record struct {
			UUID        string `json:"uuid"`
			IsSidechain bool   `json:"isSidechain"`
		}
		if json.Unmarshal(scan.Bytes(), &record) != nil || record.IsSidechain {
			continue
		}
		if anchor != "" && record.UUID == anchor {
			if !child {
				return nil
			}
			foundBoundary = true
			continue
		}
		if !child {
			continue
		}
		for _, event := range transcriptRecordEvents(scan.Bytes(), &metadata) {
			switch event.Kind {
			case "user", "assistant", "tool", "tool_result", "compact":
				if foundBoundary {
					return errors.New("Claude fork continued past the requested message")
				}
				hasConversation = true
			}
		}
	}
	if err := scan.Err(); err != nil {
		return err
	}
	if anchor != "" && !foundBoundary {
		return errors.New("Claude transcript does not contain the requested message")
	}
	if anchor == "" && !hasConversation {
		return errors.New("forked Claude transcript has no conversation")
	}
	return nil
}
