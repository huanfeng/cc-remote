package listen

import (
	"net/url"
	"slices"
	"strings"

	"cc-remote/internal/i18n"
)

// NormalizeOrigin turns "https://Example.com/" into "https://example.com", the
// form a browser sends in Origin; "" when s is not an http(s) origin.
func NormalizeOrigin(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	host := strings.ToLower(u.Host)
	// Browsers leave a default port out of Origin.
	if port := u.Port(); (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
		host = strings.TrimSuffix(host, ":"+port)
	}
	return u.Scheme + "://" + host
}

// Origins normalizes repeated and comma separated --allow-origin values,
// without duplicates; never nil.
func Origins(values []string) ([]string, error) {
	out := []string{}
	for _, v := range Split(values) {
		o := NormalizeOrigin(v)
		if o == "" {
			return nil, i18n.Errorf("invalid --allow-origin %q: expected scheme://host[:port], such as https://ccr.example.com", "无效的 --allow-origin %q：应为 scheme://host[:port]，例如 https://ccr.example.com", v)
		}
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	return out, nil
}
