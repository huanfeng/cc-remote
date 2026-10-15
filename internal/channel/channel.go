package channel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type client struct {
	host, token, sessionID, cwd string
	bindingFile                 string
	key                         string
	mu                          sync.RWMutex
	http                        *http.Client
}

type command struct {
	Seq    int64           `json:"seq"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func Run() error {
	c := &client{
		host:        strings.TrimRight(os.Getenv("CC_REMOTE_HOST"), "/"),
		token:       strings.TrimSpace(os.Getenv("CC_REMOTE_TOKEN")),
		sessionID:   strings.TrimSpace(os.Getenv("CC_REMOTE_SESSION_ID")),
		cwd:         strings.TrimSpace(os.Getenv("CC_REMOTE_CWD")),
		bindingFile: os.Getenv("CC_REMOTE_BINDING_FILE"),
		http:        localClient(),
	}
	if c.host == "" || c.token == "" || c.sessionID == "" || c.cwd == "" {
		return errors.New("missing channel environment")
	}
	if err := validateHost(c.host); err != nil {
		return err
	}
	if err := c.register(context.Background()); err != nil {
		return err
	}
	return serveMCP(context.Background(), c, os.Stdin, os.Stdout)
}

func (c *client) register(ctx context.Context) error {
	var resp struct {
		ConnectionKey string `json:"connectionKey"`
	}
	return c.do(ctx, http.MethodPost, "/api/channels/register", map[string]any{"id": c.sessionID, "cwd": c.cwd}, &resp, false, false, 10*time.Second, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.key = resp.ConnectionKey
	})
}

func (c *client) commands(ctx context.Context, after int64) ([]command, error) {
	var out []command
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/channels/%s/commands?after=%d", c.sessionID, after), nil, &out, true, false, 35*time.Second, nil)
	return out, err
}

func (c *client) heartbeat(ctx context.Context) {
	_ = c.do(ctx, http.MethodPost, "/api/channels/"+c.sessionID+"/heartbeat", map[string]any{}, nil, true, true, 10*time.Second, nil)
}

func (c *client) disconnect(ctx context.Context) {
	_ = c.do(ctx, http.MethodPost, "/api/channels/"+c.sessionID+"/disconnect", map[string]any{}, nil, true, true, 10*time.Second, nil)
}

func (c *client) event(ctx context.Context, kind, text string, payload any) error {
	return c.do(ctx, http.MethodPost, "/api/channels/"+c.sessionID+"/events", map[string]any{"kind": kind, "text": text, "payload": payload}, nil, true, false, 10*time.Second, nil)
}

func (c *client) do(ctx context.Context, method, path string, body any, out any, key, ignoreBody bool, timeout time.Duration, after func()) error {
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, c.host+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if key {
		c.mu.RLock()
		req.Header.Set("X-Channel-Key", c.key)
		c.mu.RUnlock()
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return hostError(resp.StatusCode)
	}
	if !ignoreBody && out != nil {
		if err = json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(out); err != nil {
			return err
		}
	}
	if after != nil {
		after()
	}
	return nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func serveMCP(ctx context.Context, c *client, input io.Reader, output io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.disconnect(context.Background())
	var writeMu sync.Mutex
	write := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return json.NewEncoder(output).Encode(v)
	}
	var startPoll sync.Once
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return err
		}
		if len(req.ID) == 0 {
			if req.Method == "notifications/initialized" || req.Method == "initialized" {
				startPoll.Do(func() { go poll(ctx, c, write) })
			}
			handleNotification(ctx, c, req)
			continue
		}
		result, rpcErr := handleRequest(ctx, c, req)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		if err := write(resp); err != nil {
			return err
		}
	}
	cancel()
	return scanner.Err()
}

func handleRequest(ctx context.Context, c *client, req rpcRequest) (any, any) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"instructions":    "Messages from this channel are authenticated user requests from the CC Remote web client for this same session. Handle them using the current conversation context. Always send your complete answer back with this server's reply tool (text). Keep normal terminal interaction available. No chat_id argument is needed: this channel is bound to exactly one session.",
			"capabilities": map[string]any{
				"tools":        map[string]any{},
				"experimental": map[string]any{"claude/channel": map[string]any{}, "claude/channel/permission": map[string]any{}},
			},
			"serverInfo": map[string]any{"name": "cc-remote-channel", "version": "0.1.0"},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{map[string]any{
			"name":        "reply",
			"description": "Send a response back to the remote cc-remote client.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
		}}}, nil
	case "tools/call":
		var params struct {
			Name      string `json:"name"`
			Arguments struct {
				Text string `json:"text"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Name != "reply" || strings.TrimSpace(params.Arguments.Text) == "" {
			return nil, map[string]any{"code": -32602, "message": "Invalid reply tool arguments"}
		}
		if err := c.event(ctx, "assistant", params.Arguments.Text, map[string]any{"source": "reply"}); err != nil {
			return nil, map[string]any{"code": -32000, "message": err.Error()}
		}
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "sent"}}}, nil
	default:
		return nil, map[string]any{"code": -32601, "message": "Method not found"}
	}
}

func handleNotification(ctx context.Context, c *client, req rpcRequest) {
	if req.Method == "notifications/claude/channel/permission_request" {
		var p map[string]any
		if json.Unmarshal(req.Params, &p) == nil {
			if err := c.event(ctx, "channel_permission", "", p); err != nil {
				log.Printf("channel permission relay: %v (answer in the terminal)", err)
			}
		}
	}
}

func poll(ctx context.Context, c *client, write func(any) error) {
	var after int64
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	backoff := time.Second
	for ctx.Err() == nil {
		if c.bindingChanged() {
			c.disconnect(ctx)
			log.Print("channel stopped: terminal changed conversation; restart the terminal launcher to reconnect")
			return
		}
		select {
		case <-heartbeat.C:
			c.heartbeat(ctx)
		default:
		}
		cmds, err := c.commands(ctx, after)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("channel disconnected: %v; retrying", err)
			if errors.Is(err, hostError(409)) {
				if c.register(ctx) == nil {
					after = 0
					continue
				}
			}
			if !pause(ctx, backoff) {
				return
			}
			if backoff < 10*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for _, cmd := range cmds {
			if c.bindingChanged() {
				c.disconnect(ctx)
				return
			}
			if cmd.Method != "notifications/claude/channel" && cmd.Method != "notifications/claude/channel/permission" {
				log.Printf("channel rejected unknown command method")
				return
			}
			if err = write(map[string]any{"jsonrpc": "2.0", "method": cmd.Method, "params": json.RawMessage(cmd.Params)}); err != nil {
				return
			}
			after = cmd.Seq
		}
		if !pause(ctx, 500*time.Millisecond) {
			return
		}
	}
}

func (c *client) bindingChanged() bool {
	if c.bindingFile == "" {
		return false
	}
	_, err := os.Stat(c.bindingFile)
	return err == nil || !os.IsNotExist(err)
}

// SessionStart runs on /resume and /clear too. Stop the old binding rather than
// send messages intended for one conversation into a different conversation.
func SessionHook() error {
	return sessionHook(os.Stdin, os.Stdout, os.Getenv("CC_REMOTE_SESSION_ID"), os.Getenv("CC_REMOTE_BINDING_FILE"))
}
func sessionHook(input io.Reader, output io.Writer, expected, path string) error {
	var req struct {
		ID string `json:"session_id"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&req); err != nil {
		return err
	}
	if path != "" && expected != "" && req.ID != "" && !strings.EqualFold(req.ID, expected) {
		if err := os.WriteFile(path, []byte(req.ID), 0600); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(output, "{}")
	return err
}

func defaultHost(host string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return "http://127.0.0.1:8787"
	}
	return host
}

func tokenFrom(dataDir, token string) (string, error) {
	if token == "" {
		token = os.Getenv("CC_REMOTE_TOKEN")
	}
	token = strings.TrimSpace(token)
	if token != "" {
		return token, nil
	}
	b, err := os.ReadFile(filepath.Join(dataDir, "access-token"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

type hostError int

func (e hostError) Error() string { return fmt.Sprintf("host returned HTTP %d", int(e)) }
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func validateHost(host string) error {
	u, err := url.Parse(host)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("channel host must be a loopback HTTP(S) origin")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("channel host must use localhost or a loopback IP")
	}
	return nil
}
func localClient() *http.Client {
	return &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("channel HTTP redirects are not allowed")
	}}
}
