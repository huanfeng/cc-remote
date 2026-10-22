// Package hostauth lets a local client check that the host answering a URL
// knows the access token before sending it: while the host is down, another
// local user could listen on its port to collect the token.
package hostauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"cc-remote/internal/hostlock"
)

// Path is the host's proof endpoint; it needs no token.
const Path = "/api/hello"

// Marker is the file a host that answers Path writes in its data directory
// when it starts: from then on, nothing in its place is trusted without the proof.
const Marker = "host-proof"

// MarkHost records that a host answering Path runs from dataDir.
func MarkHost(dataDir string) error {
	return os.WriteFile(filepath.Join(dataDir, Marker), []byte("1\n"), 0o600)
}

// Allowed reports whether the token may go to base: this machine (where Check
// proves the host) or an HTTPS host, never plain HTTP across the network.
func Allowed(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && Local(base)
}

var nonceRE = regexp.MustCompile(`^[0-9a-f]{32,128}$`)

// ValidNonce reports whether a request's nonce has the form clients send.
func ValidNonce(nonce string) bool { return nonceRE.MatchString(nonce) }

// Proof is what a host holding token answers for nonce.
func Proof(token, nonce string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("cc-remote host proof\x00" + nonce))
	return hex.EncodeToString(mac.Sum(nil))
}

var (
	// ErrWrongToken: a CC Remote host answers but holds another token.
	ErrWrongToken = errors.New("the host does not hold this access token")
	// ErrNotHost: whatever answers cannot show it is this data directory's host.
	ErrNotHost = errors.New("the service answering is not this data directory's host")
)

// Local reports whether base names this machine (localhost or a loopback IP).
func Local(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()
}

// Verify is Check for a host on this machine, where another user can take the
// port; an HTTPS host elsewhere is vouched for by its certificate.
func Verify(client *http.Client, base, token, dataDir string) error {
	if !Local(base) && strings.HasPrefix(strings.ToLower(base), "https://") {
		return nil
	}
	return Check(client, base, token, dataDir)
}

// Check returns nil when the host at base proves it holds token. A host too
// old to prove it is accepted while it holds dataDir's lock (it is the one
// running), and only until a newer host has run there (Marker): the lock does
// not say where that host listens. Other errors mean nothing answered.
func Check(client *http.Client, base, token, dataDir string) error {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	nonce := hex.EncodeToString(b)
	resp, err := client.Get(base + Path + "?nonce=" + nonce)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var v struct {
			Proof string `json:"proof"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&v) != nil {
			return ErrNotHost
		}
		if token == "" || !hmac.Equal([]byte(v.Proof), []byte(Proof(token, nonce))) {
			return ErrWrongToken
		}
		return nil
	case http.StatusUnauthorized, http.StatusNotFound: // a host before /api/hello
		if _, err := os.Stat(filepath.Join(dataDir, Marker)); dataDir != "" && errors.Is(err, os.ErrNotExist) && hostlock.Held(dataDir) {
			return nil
		}
	}
	return ErrNotHost
}
