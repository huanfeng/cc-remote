package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type unavailableProfile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	App    string `json:"app"`
	Reason string `json:"reason"`
}

func (s *Server) ccSwitchProfiles() (profileFile, error) {
	f := profileFile{Version: 1, Source: "cc-switch", DefaultProfiles: map[string]string{}, Profiles: []supplierProfile{}, Unavailable: []unavailableProfile{}}
	path := s.cfg.CCSwitchDBPath
	if path == "" {
		path = os.Getenv("CC_REMOTE_CC_SWITCH_DB")
	}
	if path == "" {
		home, e := os.UserHomeDir()
		if e != nil {
			return f, errors.New("无法确定 CC-Switch 数据目录")
		}
		path = filepath.Join(home, ".cc-switch", "cc-switch.db")
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil || info.IsDir() {
		return f, errors.New("无法读取 CC-Switch 数据库")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return f, errors.New("CC-Switch 路径无效")
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	query := url.Values{"mode": {"ro"}, "_pragma": {"query_only(1)", "busy_timeout(3000)"}}
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return f, errors.New("无法打开 CC-Switch 只读数据库")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.Begin()
	if err != nil {
		return f, errors.New("无法读取 CC-Switch 数据库")
	}
	defer tx.Rollback()
	columns := map[string]bool{}
	rows, err := tx.Query("PRAGMA table_info(providers)")
	if err != nil {
		return f, errors.New("无法读取 CC-Switch 供应商结构")
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err = rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			rows.Close()
			return f, errors.New("CC-Switch 供应商结构无效")
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return f, errors.New("无法读取 CC-Switch 供应商结构")
	}
	for _, name := range []string{"id", "app_type", "name", "settings_config"} {
		if !columns[name] {
			return f, errors.New("CC-Switch 缺少必需的供应商字段：" + name)
		}
	}
	common := map[string]string{}
	var hasSettings int
	if err = tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='settings'").Scan(&hasSettings); err != nil {
		return f, errors.New("无法读取 CC-Switch 公共配置")
	}
	if hasSettings > 0 {
		r, e := tx.Query("SELECT key,value FROM settings WHERE key IN ('common_config_claude','common_config_codex')")
		if e != nil {
			return f, errors.New("CC-Switch 公共配置结构不兼容")
		}
		for r.Next() {
			var k string
			var v sql.NullString
			if e = r.Scan(&k, &v); e != nil {
				r.Close()
				return f, errors.New("CC-Switch 公共配置无效")
			}
			common[strings.TrimPrefix(k, "common_config_")] = v.String
		}
		e = r.Err()
		r.Close()
		if e != nil {
			return f, errors.New("无法读取 CC-Switch 公共配置")
		}
	}
	meta, current, order := "'{}'", "0", "app_type,name,id"
	if columns["meta"] {
		meta = "COALESCE(meta,'{}')"
	}
	if columns["is_current"] {
		current = "COALESCE(is_current,0)"
	}
	if columns["sort_index"] {
		order = "app_type,COALESCE(sort_index,2147483647),name,id"
	}
	rows, err = tx.Query("SELECT id,app_type,name,settings_config," + meta + "," + current + " FROM providers WHERE app_type IN ('claude','codex') ORDER BY " + order)
	if err != nil {
		return f, errors.New("无法查询 CC-Switch 供应商")
	}
	seen := map[string]bool{}
	for rows.Next() {
		var id, app, name, raw, metadata string
		var selected bool
		if err = rows.Scan(&id, &app, &name, &raw, &metadata, &selected); err != nil {
			rows.Close()
			return f, errors.New("CC-Switch 供应商数据无效")
		}
		if id == "" || name == "" || seen[app+":"+id] {
			rows.Close()
			return f, errors.New("CC-Switch 供应商 ID 或名称无效/重复")
		}
		seen[app+":"+id] = true
		p, e := normalizeCCSwitch(id, name, app, raw, metadata, common[app])
		if e != nil {
			f.Unavailable = append(f.Unavailable, unavailableProfile{"ccswitch:" + id, name, app, e.Error()})
			continue
		}
		f.Profiles = append(f.Profiles, p)
		if selected {
			f.DefaultProfiles[app] = p.ID
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return f, errors.New("无法查询 CC-Switch 供应商")
	}
	if err = tx.Commit(); err != nil {
		return f, errors.New("无法完成 CC-Switch 只读查询")
	}
	// Device-local current-provider selections take precedence over shared DB flags.
	b, err := os.ReadFile(filepath.Join(filepath.Dir(path), "settings.json"))
	if err == nil {
		var local map[string]json.RawMessage
		if json.Unmarshal(b, &local) != nil {
			return f, errors.New("CC-Switch settings.json 无效")
		}
		for app, key := range map[string]string{"claude": "currentProviderClaude", "codex": "currentProviderCodex"} {
			var id string
			if json.Unmarshal(local[key], &id) != nil || id == "" {
				continue
			}
			for _, p := range f.Profiles {
				if p.App == app && p.ID == "ccswitch:"+id {
					f.DefaultProfiles[app] = p.ID
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return f, errors.New("无法读取 CC-Switch settings.json")
	}
	f.Found = true
	return f, nil
}

func normalizeCCSwitch(id, name, app, raw, metadata, common string) (supplierProfile, error) {
	p := supplierProfile{ID: "ccswitch:" + id, Name: name, App: app}
	var settings map[string]any
	var meta map[string]any
	if len(raw) > 4<<20 || json.Unmarshal([]byte(raw), &settings) != nil || json.Unmarshal([]byte(metadata), &meta) != nil {
		return p, errors.New("供应商 JSON 配置无效")
	}
	enabled, _ := meta["commonConfigEnabled"].(bool)
	if app == "claude" {
		if enabled && strings.TrimSpace(common) != "" {
			var base map[string]any
			if json.Unmarshal([]byte(common), &base) != nil {
				return p, errors.New("Claude 公共配置无效")
			}
			settings = mergeProfileMaps(base, settings)
		}
		p.Settings.Env = map[string]string{}
		env, _ := settings["env"].(map[string]any)
		for k, v := range env {
			if !(strings.HasPrefix(k, "ANTHROPIC_") || k == "API_TIMEOUT_MS" || k == "CLAUDE_CODE_SUBAGENT_MODEL") {
				continue
			}
			value, ok := v.(string)
			if !ok {
				return p, errors.New("Claude 连接变量必须是字符串")
			}
			p.Settings.Env[k] = value
		}
		if p.Settings.Env["ANTHROPIC_BASE_URL"] == "" && (p.Settings.Env["ANTHROPIC_API_KEY"] != "" || p.Settings.Env["ANTHROPIC_AUTH_TOKEN"] != "") {
			p.Settings.Env["ANTHROPIC_BASE_URL"] = "https://api.anthropic.com"
		}
		if p.Settings.Env["ANTHROPIC_API_KEY"] == "" && p.Settings.Env["ANTHROPIC_AUTH_TOKEN"] == "" {
			return p, errors.New("无 API 凭据；官方登录请使用宿主原生配置")
		}
	} else {
		auth, _ := settings["auth"].(map[string]any)
		key, _ := auth["OPENAI_API_KEY"].(string)
		if key == "" {
			return p, errors.New("无 API Key；官方登录请使用宿主原生配置")
		}
		p.Settings.Auth = map[string]string{"OPENAI_API_KEY": key}
		config, e := codexConnectionConfig(settings)
		if e != nil {
			return p, e
		}
		if enabled && strings.TrimSpace(common) != "" {
			base, e := readConnectionTOML(common)
			if e != nil {
				return p, errors.New("Codex 公共连接配置不受支持")
			}
			config = mergeProfileMaps(base, config)
		}
		selected, _ := config["model_provider"].(string)
		if selected == "" {
			selected = "openai"
			config["model_provider"] = selected
		}
		providers, _ := config["model_providers"].(map[string]any)
		if providers == nil {
			providers = map[string]any{}
			config["model_providers"] = providers
		}
		connection, _ := providers[selected].(map[string]any)
		if connection == nil && selected == "openai" {
			connection = map[string]any{"name": "OpenAI", "base_url": "https://api.openai.com/v1", "wire_api": "responses"}
			providers[selected] = connection
		}
		if connection == nil {
			return p, errors.New("Codex 缺少所选供应商的连接定义")
		}
		connection["env_key"] = "OPENAI_API_KEY"
		connection["requires_openai_auth"] = false
		p.Settings.ConfigOverrides = config
	}
	if err := p.validate(); err != nil {
		return p, err
	}
	return p, nil
}

func mergeProfileMaps(base, patch map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for k, v := range patch {
		if sub, ok := v.(map[string]any); ok {
			old, _ := base[k].(map[string]any)
			base[k] = mergeProfileMaps(old, sub)
		} else {
			base[k] = v
		}
	}
	return base
}
