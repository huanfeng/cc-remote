package main

import (
	"errors"
	"log"
	"maps"
	"os"
	"strings"

	"cc-remote/internal/i18n"
	"cc-remote/internal/listen"
	"cc-remote/internal/server"
	"cc-remote/internal/service"
)

// applyServiceConfig makes the saved service settings override the command
// line the service manager was registered with, which may be older.
func applyServiceConfig(o *hostOptions, c *service.Config) {
	if len(c.Listen) > 0 {
		o.listen = stringList(c.Listen)
	}
	if c.Claude != "" {
		*o.claudeBin = c.Claude
	}
	*o.configDir = c.ClaudeConfigDir
	*o.allowInsecureRemote = c.AllowInsecureRemote
	o.allowOrigin = stringList(c.AllowOrigins)
	// Empty is "auto": what it stood for when saved, else the registered --lang.
	if lang := effectiveLanguage(c); lang != "" {
		*o.lang = lang
	}
	// An older service definition may still pass on removed variables.
	for _, k := range c.EnvRemoved {
		os.Unsetenv(k)
	}
	for k, v := range c.Env {
		if k != "CC_REMOTE_SERVICE_MANAGER" {
			os.Setenv(k, v)
		}
	}
}

// reloadSettings applies a changed service.json to the running host: listen
// addresses take effect now; the other settings it reports need a restart.
func reloadSettings(dataDir string, started *service.Config, lang i18n.Lang, ls *listenerSet) (server.ReloadResult, error) {
	c, err := service.LoadConfig(dataDir)
	if err != nil {
		return server.ReloadResult{}, err
	}
	if c == nil {
		return server.ReloadResult{}, errors.New(i18n.T("service.json not found in the data directory", "数据目录中找不到 service.json"))
	}
	restart := restartSettings(started, c, lang)
	// Web frontends apply at once, like listen addresses; CC_REMOTE_ALLOW_ORIGINS
	// is the environment the host started with (env changes need a restart).
	origins, err := listen.Origins(append([]string{os.Getenv("CC_REMOTE_ALLOW_ORIGINS")}, c.AllowOrigins...))
	if err != nil {
		return server.ReloadResult{}, err
	}
	specs := listen.Split(c.Listen)
	if len(specs) == 0 {
		specs = []string{"127.0.0.1:8787"}
	}
	if bad := listen.Insecure(specs); len(bad) > 0 && !c.AllowInsecureRemote {
		return server.ReloadResult{}, i18n.Errorf("--listen %s binds every interface over plain HTTP; use lan:PORT or a specific address, or pass --allow-insecure-remote if you really mean it", "--listen %s 会在所有网卡上提供明文 HTTP；请改用 lan:PORT 或具体地址，确有需要时加 --allow-insecure-remote", strings.Join(bad, ","))
	}
	addrs, added, err := listen.Resolve(specs, nil)
	if err != nil {
		return server.ReloadResult{}, err
	}
	bound, err := ls.apply(addrs)
	if errors.Is(err, errListenRestart) {
		return server.ReloadResult{Listen: ls.bound(), Restart: append(restart, "listen"), AllowOrigins: origins}, nil
	}
	if err != nil {
		return server.ReloadResult{Listen: ls.bound()}, err
	}
	if added != "" {
		log.Printf("No loopback listen address configured; also listening on %s for local tools", added)
	}
	warnExposure(bound)
	return server.ReloadResult{Listen: bound, Restart: restart, AllowOrigins: origins}, nil
}

// effectiveLanguage is the language the saved settings ask for, or empty.
func effectiveLanguage(c *service.Config) string {
	if c.Language != "" {
		return c.Language
	}
	return c.AutoLanguage
}

// restartSettings names the settings that differ from the ones the host
// started with (running in language lang) and only apply on a restart.
func restartSettings(started, c *service.Config, lang i18n.Lang) []string {
	if started == nil {
		return []string{"service.json"}
	}
	var names []string
	if c.Claude != started.Claude {
		names = append(names, "claude")
	}
	if c.ClaudeConfigDir != started.ClaudeConfigDir {
		names = append(names, "claude-config-dir")
	}
	if l := effectiveLanguage(c); l != "" && l != string(lang) {
		names = append(names, "lang")
	}
	if !maps.Equal(c.Env, started.Env) {
		names = append(names, "env")
	}
	return names
}
