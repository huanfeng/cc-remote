package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

type supplierProbeRequest struct {
	ID         string `json:"id"`
	App        string `json:"app"`
	BaseURL    string `json:"baseUrl"`
	AuthType   string `json:"authType"`
	Credential string `json:"credential"`
}
type supplierProbeReply struct {
	Models    []string `json:"models"`
	LatencyMs int64    `json:"latencyMs"`
	Truncated bool     `json:"truncated"`
}

// Supplier probes never expose upstream bodies, request errors, or stored credentials.
func (s *Server) supplierProbeAPI(w http.ResponseWriter, r *http.Request) {
	var q supplierProbeRequest
	if !decode(w, r, &q) {
		return
	}
	if (q.App != "claude" && q.App != "codex") || (q.AuthType != "apiKey" && q.AuthType != "authToken") {
		fail(w, 400, "Invalid application or authentication type")
		return
	}
	base, err := modelsBaseURL(s.lang(r), q.BaseURL)
	if err != nil || strings.TrimSpace(q.BaseURL) == "" {
		fail(w, 400, "请使用 HTTPS 地址（本机地址可使用 HTTP） / Use HTTPS except for localhost")
		return
	}
	if q.Credential == "" && q.ID != "" {
		supplierConfigsMu.Lock()
		configs, _, readErr := readSupplierConfigs(s.cfg.DataDir)
		supplierConfigsMu.Unlock()
		if readErr != nil {
			fail(w, 500, "Could not read supplier configuration")
			return
		}
		for _, p := range configs {
			if p.ID == q.ID && p.App == q.App && strings.TrimRight(p.BaseURL, "/") == strings.TrimRight(q.BaseURL, "/") && p.AuthType == q.AuthType {
				q.Credential = p.Credential
				break
			}
		}
	}
	if q.Credential == "" || len(q.Credential) > 8192 || strings.IndexFunc(q.Credential, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		fail(w, 400, "请填写凭据；端点或认证变更后需重新输入 / Enter a credential; endpoint or authentication changes require re-entry")
		return
	}
	started := time.Now()
	result, err := probeSupplier(r.Context(), base, q)
	if err != nil {
		fail(w, 502, err.Error())
		return
	}
	result.LatencyMs = time.Since(started).Milliseconds()
	respond(w, 200, result)
}

func probeSupplier(parent context.Context, base string, q supplierProbeRequest) (supplierProbeReply, error) {
	out := supplierProbeReply{Models: []string{}}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(base, "/")
	if q.App == "claude" && !strings.HasSuffix(endpoint, "/v1") {
		endpoint += "/v1"
	}
	endpoint += "/models"
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		target := endpoint
		if q.App == "claude" {
			v := url.Values{"limit": {"1000"}}
			if cursor != "" {
				v.Set("after_id", cursor)
			}
			target += "?" + v.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return out, fmt.Errorf("Invalid API endpoint")
		}
		if q.App == "claude" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		if q.App == "claude" && q.AuthType == "apiKey" {
			req.Header.Set("x-api-key", q.Credential)
		} else {
			req.Header.Set("Authorization", "Bearer "+q.Credential)
		}
		resp, err := modelsClient.Do(req)
		if err != nil {
			return out, fmt.Errorf("API 请求失败或超时 / API request failed or timed out")
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return out, fmt.Errorf("模型列表检测失败 / Model list check failed (HTTP %d)", resp.StatusCode)
		}
		var body struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body)
		resp.Body.Close()
		if err != nil || body.Data == nil {
			return out, fmt.Errorf("模型接口未返回有效列表 / Model endpoint did not return a valid list")
		}
		for _, m := range body.Data {
			if m.ID != "" && len(m.ID) <= 160 && !seen[m.ID] && !strings.Contains(m.ID, q.Credential) && strings.IndexFunc(m.ID, unicode.IsControl) < 0 {
				seen[m.ID] = true
				out.Models = append(out.Models, m.ID)
			}
		}
		if !body.HasMore {
			break
		}
		if body.LastID == "" || body.LastID == cursor {
			out.Truncated = true
			break
		}
		cursor = body.LastID
		out.Truncated = page == 9
	}
	sort.Strings(out.Models)
	return out, nil
}
