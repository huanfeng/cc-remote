package terminal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"cc-remote/internal/i18n"
	"golang.org/x/term"
)

type supplierItem struct {
	ID            string            `json:"id,omitempty"`
	Website       string            `json:"website"`
	Notes         string            `json:"notes"`
	ModelMappings map[string]string `json:"modelMappings"`
	ModelNames    map[string]string `json:"modelNames,omitempty"`
	TimeoutMs     int64             `json:"timeoutMs"`
	CodexOptions  map[string]any    `json:"codexOptions"`
	HasCredential bool              `json:"hasCredential,omitempty"`
	Name          string            `json:"name"`
	App           string            `json:"app"`
	BaseURL       string            `json:"baseUrl"`
	Model         string            `json:"model"`
	AuthType      string            `json:"authType"`
	Credential    string            `json:"credential,omitempty"`
	Revision      string            `json:"revision"`
}
type supplierCollection struct {
	Profiles     []supplierItem  `json:"profiles"`
	Capabilities map[string]bool `json:"capabilities"`
	Revision     string          `json:"revision"`
}
type supplierOptions struct {
	Name, BaseURL, Model, AuthType string
	CredentialStdin                bool
	JSON                           bool
	Website, Notes                 string
	AdvancedStrings                map[string]*string
	AdvancedInts                   map[string]*int64
	DisableStorage                 bool
	Set                            map[string]bool
}

func validProfileApp(app string) error {
	if app != "claude" && app != "codex" {
		return i18n.Errorf("invalid app %q: use claude or codex", "应用 %q 无效：请选择 claude 或 codex", app)
	}
	return nil
}

func readSupplierCredential(forceStdin bool) (string, error) {
	if !forceStdin {
		if v := strings.TrimSpace(os.Getenv("CC_REMOTE_SUPPLIER_CREDENTIAL")); v != "" {
			return v, nil
		}
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprint(os.Stderr, i18n.T("Supplier credential (hidden): ", "供应商凭据（隐藏输入）："))
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return strings.TrimSpace(string(b)), err
		}
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 8194))
	return strings.TrimSpace(string(b)), err
}

func runSupplier(api doer, out io.Writer, sub, app string, names []string, opts supplierOptions, readCredential func(bool) (string, error)) error {
	if err := validProfileApp(app); err != nil {
		return err
	}
	var list supplierCollection
	if err := api.do("GET", "/api/supplier-configs", nil, &list); err != nil {
		var refused hostError
		if errors.As(err, &refused) && (refused.status == 404 || refused.status == 405) {
			return errors.New(i18n.T("this host does not support supplier configuration; update the host first", "此宿主不支持供应商配置，请先升级宿主"))
		}
		return err
	}
	for _, key := range supplierAdvancedFlags {
		if opts.Set[key] && (key == "fable-model" || strings.HasSuffix(key, "-model-name")) && !list.Capabilities["modelNames"] {
			return errors.New(i18n.T("upgrade this host for model display names and Fable", "请升级宿主以配置模型显示名称和 Fable"))
		}
		if opts.Set[key] && !list.Capabilities["advanced"] {
			return errors.New(i18n.T("upgrade this host for advanced supplier options", "请升级宿主以使用高级供应商配置"))
		}
	}
	req := supplierItem{App: app, AuthType: "apiKey"}
	if app == "claude" {
		req.AuthType = "authToken"
	}
	method, path := "POST", "/api/supplier-configs"
	if sub != "add" {
		// Resolve against every supplier so imported and OAuth entries cannot be edited,
		// and duplicate names never silently select a different account.
		var profiles profileList
		if err := api.do("GET", "/api/profiles", nil, &profiles); err != nil {
			return err
		}
		id, err := matchProfile(profiles, app, names[0])
		if err != nil {
			return err
		}
		if !strings.HasPrefix(id, "local:") {
			return errors.New(i18n.T("only local API suppliers can be edited or removed here; use ccr oauth for OAuth accounts and CC-Switch for imported entries", "此命令仅管理本地 API 供应商；OAuth 账号请用 ccr oauth，导入项请在 CC-Switch 中管理"))
		}
		found := false
		for _, p := range list.Profiles {
			if p.ID == id && p.App == app {
				req = p
				found = true
				break
			}
		}
		if !found {
			return errors.New(i18n.T("supplier changed; run the command again", "供应商已发生变化，请重新执行命令"))
		}
		method, path = "PUT", path+"/"+url.PathEscape(id)
	}
	req.Revision = list.Revision
	req.Credential = ""
	if sub == "show" {
		b, e := json.MarshalIndent(req, "", "  ")
		if e != nil {
			return e
		}
		fmt.Fprintln(out, string(b))
		return nil
	}
	if sub == "rm" || sub == "remove" {
		if err := api.do("DELETE", path, map[string]string{"revision": list.Revision}, nil); err != nil {
			return err
		}
		fmt.Fprintln(out, i18n.T("Supplier removed.", "供应商已删除。"))
		return nil
	}
	if sub == "add" || opts.Set["name"] {
		req.Name = strings.TrimSpace(opts.Name)
	}
	if sub == "add" || opts.Set["base-url"] {
		req.BaseURL = strings.TrimSpace(opts.BaseURL)
	}
	if sub == "add" || opts.Set["model"] {
		req.Model = strings.TrimSpace(opts.Model)
	}
	if opts.Set["auth-type"] {
		req.AuthType = opts.AuthType
	}
	if opts.Set["website"] {
		req.Website = opts.Website
	}
	if opts.Set["notes"] {
		req.Notes = opts.Notes
	}
	if req.ModelMappings == nil {
		req.ModelMappings = map[string]string{}
	}
	if req.ModelNames == nil {
		req.ModelNames = map[string]string{}
	}
	if req.CodexOptions == nil {
		req.CodexOptions = map[string]any{}
	}
	for flag, value := range opts.AdvancedStrings {
		if !opts.Set[flag] {
			continue
		}
		switch flag {
		case "haiku-model", "sonnet-model", "opus-model", "fable-model":
			req.ModelMappings[strings.TrimSuffix(flag, "-model")] = *value
		case "haiku-model-name", "sonnet-model-name", "opus-model-name", "fable-model-name":
			role := strings.TrimSuffix(flag, "-model-name")
			req.ModelNames[role] = *value
		default:
			key := map[string]string{"reasoning-effort": "reasoningEffort", "reasoning-summary": "reasoningSummary", "verbosity": "verbosity"}[flag]
			if *value == "" {
				delete(req.CodexOptions, key)
			} else {
				req.CodexOptions[key] = *value
			}
		}
	}
	for flag, value := range opts.AdvancedInts {
		if !opts.Set[flag] {
			continue
		}
		if flag == "timeout-ms" {
			req.TimeoutMs = *value
		} else {
			key := map[string]string{"context-window": "contextWindow", "auto-compact-token-limit": "autoCompactTokenLimit"}[flag]
			if *value == 0 {
				delete(req.CodexOptions, key)
			} else {
				req.CodexOptions[key] = *value
			}
		}
	}
	if opts.Set["disable-response-storage"] {
		req.CodexOptions["disableResponseStorage"] = opts.DisableStorage
	}
	if req.Name == "" || req.BaseURL == "" {
		return errors.New(i18n.T("--name and --base-url must be nonempty", "--name 与 --base-url 不能为空"))
	}
	if (req.AuthType != "apiKey" && req.AuthType != "authToken") || (app == "codex" && req.AuthType != "apiKey") {
		return errors.New(i18n.T("use apiKey or authToken; Codex supports apiKey only", "认证方式请选择 apiKey 或 authToken；Codex 仅支持 apiKey"))
	}
	if sub == "add" || opts.CredentialStdin {
		credential, err := readCredential(opts.CredentialStdin)
		if err != nil {
			return err
		}
		req.Credential = strings.TrimSpace(credential)
		if sub == "add" && req.Credential == "" {
			return errors.New(i18n.T("supplier credential is required", "请提供供应商凭据"))
		}
	}
	if err := api.do(method, path, req, nil); err != nil {
		return err
	}
	fmt.Fprintln(out, i18n.T("Supplier saved. Use ccr profile default NAME to select it for new sessions.", "供应商已保存。使用 ccr profile default 名称可将其设为新会话默认项。"))
	return nil
}

var supplierAdvancedFlags = []string{"website", "notes", "haiku-model", "sonnet-model", "opus-model", "fable-model", "haiku-model-name", "sonnet-model-name", "opus-model-name", "fable-model-name", "timeout-ms", "reasoning-effort", "reasoning-summary", "verbosity", "context-window", "auto-compact-token-limit", "disable-response-storage"}
