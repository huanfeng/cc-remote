package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	minGzipSize = 1024
	maxBuffered = 8 << 20 // larger responses stream through unchanged
)

var gzipWriters = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed); return w }}

// condensed buffers GET responses so they can carry an ETag (a polling client
// that sends If-None-Match gets 304 when nothing changed) and be gzipped for
// clients that accept it: JSON transcripts shrink several times, which is what
// matters on slow links. Attachments and range requests pass through as is.
func condensed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Range") != "" || strings.Contains(r.URL.Path, "/uploads/") {
			next.ServeHTTP(w, r)
			return
		}
		b := &bufferedResponse{header: w.Header(), out: w}
		next.ServeHTTP(b, r)
		if b.streaming {
			return
		}
		h := w.Header()
		status := b.status
		if status == 0 {
			status = http.StatusOK
		}
		body := b.buf.Bytes()
		compressible := compressibleType(h.Get("Content-Type")) && h.Get("Content-Encoding") == ""
		if compressible {
			h.Add("Vary", "Accept-Encoding")
		}
		if status == http.StatusOK {
			sum := sha256.Sum256(body)
			tag := `W/"` + hex.EncodeToString(sum[:12]) + `"`
			h.Set("ETag", tag)
			if etagMatches(r.Header.Get("If-None-Match"), tag) {
				h.Del("Content-Length")
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if compressible && len(body) >= minGzipSize && acceptsGzip(r.Header.Get("Accept-Encoding")) {
			var out bytes.Buffer
			gz := gzipWriters.Get().(*gzip.Writer)
			gz.Reset(&out)
			_, _ = gz.Write(body)
			if gz.Close() == nil {
				body = out.Bytes()
				h.Set("Content-Encoding", "gzip")
			}
			gzipWriters.Put(gz)
		}
		if status != http.StatusNotModified && status != http.StatusNoContent {
			h.Set("Content-Length", strconv.Itoa(len(body)))
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

type bufferedResponse struct {
	header    http.Header
	status    int
	buf       bytes.Buffer
	out       http.ResponseWriter
	streaming bool // past maxBuffered: writes go straight to out
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	if b.streaming {
		return b.out.Write(p)
	}
	if b.buf.Len()+len(p) > maxBuffered {
		b.streaming = true
		b.out.WriteHeader(b.status)
		if _, err := b.out.Write(b.buf.Bytes()); err != nil {
			return 0, err
		}
		b.buf = bytes.Buffer{}
		return b.out.Write(p)
	}
	return b.buf.Write(p)
}

func compressibleType(contentType string) bool {
	t, _, _ := strings.Cut(contentType, ";")
	t = strings.TrimSpace(strings.ToLower(t))
	return strings.HasPrefix(t, "text/") || t == "application/json" || t == "application/javascript" || t == "image/svg+xml" || t == "application/manifest+json"
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		if name = strings.TrimSpace(name); !strings.EqualFold(name, "gzip") && name != "*" {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(param), "="); ok && strings.EqualFold(k, "q") {
				if q, err := strconv.ParseFloat(v, 64); err == nil && q == 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

func etagMatches(header, tag string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.TrimPrefix(part, "W/") == strings.TrimPrefix(tag, "W/") {
			return true
		}
	}
	return false
}
