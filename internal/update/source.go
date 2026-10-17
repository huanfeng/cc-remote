package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cc-remote/internal/i18n"
)

// DefaultRepo is the GitHub repository releases are fetched from; the
// CC_REMOTE_REPO environment variable overrides it.
const DefaultRepo = "huanfeng/cc-remote"

// Release is the subset of a GitHub release the updater needs.
type Release struct {
	Tag    string
	Assets []Asset
}

// Asset is one downloadable file of a release.
type Asset struct {
	ID   int64
	Name string
}

// source fetches release metadata and assets. Two implementations exist:
// the gh CLI (uses its stored login) and the REST API with a token.
type source interface {
	// release returns the release for tag, or the latest one when tag is "".
	release(ctx context.Context, tag string) (Release, error)
	// download saves asset into dir and returns the written path.
	download(ctx context.Context, rel Release, a Asset, dir string) (string, error)
	name() string
}

type notFoundError struct{}

func (notFoundError) Error() string { return i18n.T("release not found", "找不到该版本") }

var errNotFound error = notFoundError{}

// authHelp is shown when no usable credentials exist for the private repo.
func authHelp(repo string) string {
	return i18n.F(`cannot access GitHub releases of %s.
The repository is private, so downloads need credentials. Either:
  1. install the GitHub CLI and run "gh auth login", or
  2. set GITHUB_TOKEN (or GH_TOKEN) to a fine-grained token with "Contents: Read" on this repository.`, `无法访问 %s 的 GitHub 发布版本。
该仓库是私有的，下载需要凭据。任选其一：
  1. 安装 GitHub CLI 并运行 "gh auth login"，或
  2. 把 GITHUB_TOKEN（或 GH_TOKEN）设为对该仓库有 "Contents: Read" 权限的细粒度令牌。`, repo)
}

// pickSource prefers an authenticated gh CLI, then a token from the
// environment, then anonymous API access (works only for public repos).
func pickSource(repo string) source {
	if gh, err := exec.LookPath("gh"); err == nil {
		if exec.Command(gh, "auth", "status", "--hostname", "github.com").Run() == nil {
			return &ghSource{bin: gh, repo: repo}
		}
	}
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	return &apiSource{base: "https://api.github.com", repo: repo, token: token, client: newHTTPClient()}
}

// newHTTPClient bounds connecting and waiting for response headers; the body
// transfer is bounded by the caller's context (see downloadTimeout).
func newHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = 30 * time.Second
	t.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: t}
}

type ghSource struct {
	bin, repo string
}

func (g *ghSource) name() string { return "gh CLI" }

func (g *ghSource) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, g.bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(strings.ToLower(msg), "not found") {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("gh %s: %v: %s", args[0]+" "+args[1], err, msg)
	}
	return out, nil
}

func (g *ghSource) release(ctx context.Context, tag string) (Release, error) {
	args := []string{"release", "view"}
	if tag != "" {
		args = append(args, tag)
	}
	args = append(args, "-R", g.repo, "--json", "tagName,assets")
	out, err := g.run(ctx, args...)
	if err != nil {
		return Release{}, err
	}
	var data struct {
		TagName string `json:"tagName"`
		Assets  []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return Release{}, i18n.Errorf("parse gh release view output: %w", "解析 gh release view 输出失败：%w", err)
	}
	rel := Release{Tag: data.TagName}
	for _, a := range data.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name})
	}
	return rel, nil
}

func (g *ghSource) download(ctx context.Context, rel Release, a Asset, dir string) (string, error) {
	if _, err := g.run(ctx, "release", "download", rel.Tag, "-R", g.repo, "-p", a.Name, "-D", dir, "--clobber"); err != nil {
		return "", err
	}
	return filepath.Join(dir, a.Name), nil
}

type apiSource struct {
	base, repo, token string
	client            *http.Client
}

func (s *apiSource) name() string {
	if s.token == "" {
		return "GitHub API (anonymous)"
	}
	return "GitHub API (token)"
}

func (s *apiSource) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "cc-remote-updater")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	// Go drops the Authorization header when GitHub redirects the asset
	// download to its storage host, which is what that host expects.
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		if s.token == "" && !s.repoVisible(ctx) {
			// GitHub answers 404 for private repos without credentials.
			return nil, errors.New(authHelp(s.repo))
		}
		return nil, errNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, errors.New(i18n.F("GitHub API %s: %s (check the token and its Contents: Read permission)\n", "GitHub API %s：%s（请检查令牌及其 Contents: Read 权限）\n", resp.Status, strings.TrimSpace(string(body))) + authHelp(s.repo))
	}
	return nil, fmt.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(string(body)))
}

// repoVisible reports whether the repository itself answers, which tells a
// missing release (404 on a readable repo) from a private repo without
// credentials (404 everywhere).
func (s *apiSource) repoVisible(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/repos/"+s.repo, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cc-remote-updater")
	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (s *apiSource) release(ctx context.Context, tag string) (Release, error) {
	url := s.base + "/repos/" + s.repo + "/releases/latest"
	if tag != "" {
		url = s.base + "/repos/" + s.repo + "/releases/tags/" + tag
	}
	resp, err := s.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	var data struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return Release{}, i18n.Errorf("parse release JSON: %w", "解析版本 JSON 失败：%w", err)
	}
	rel := Release{Tag: data.TagName}
	for _, a := range data.Assets {
		rel.Assets = append(rel.Assets, Asset{ID: a.ID, Name: a.Name})
	}
	return rel, nil
}

func (s *apiSource) download(ctx context.Context, _ Release, a Asset, dir string) (string, error) {
	resp, err := s.get(ctx, fmt.Sprintf("%s/repos/%s/releases/assets/%d", s.base, s.repo, a.ID), "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	path := filepath.Join(dir, filepath.Base(a.Name))
	if err := writeEntry(path, resp.Body, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
