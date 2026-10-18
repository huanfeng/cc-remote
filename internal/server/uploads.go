package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

const (
	maxUploadBytes     = 20 << 20
	maxAttachments     = 8
	maxUploadNameBytes = 120
)

var imageMIMEs = map[string]string{"image/png": "png", "image/jpeg": "jpeg", "image/gif": "gif"}

// Kind "image" (or empty, from older metadata) is a validated PNG/JPEG/GIF stored as
// <id>.<format>; kind "file" is stored as-is as <id>-<sanitized name>.
type attachment struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// sanitizeUploadName keeps a display/storage name without path components,
// control or reserved characters, limited to maxUploadNameBytes with its extension.
func sanitizeUploadName(raw string) string {
	raw = raw[strings.LastIndexAny(raw, `/\`)+1:]
	name := strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, raw)
	name = strings.Trim(name, " .")
	if len(name) > maxUploadNameBytes {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		stem := strings.TrimSuffix(name, ext)
		cut := maxUploadNameBytes - len(ext)
		for cut > 0 && !utf8.RuneStart(stem[cut]) {
			cut--
		}
		name = strings.TrimRight(stem[:cut], " .") + ext
	}
	if name == "" || strings.Trim(name, ".") == "" {
		return "file"
	}
	return name
}

func (s *Server) uploadPaths(l i18n.Lang, sessionID, uploadID string) (string, string, error) {
	if !channelUUID.MatchString(sessionID) || !channelUUID.MatchString(uploadID) || !filepath.IsAbs(s.cfg.UploadDir) {
		return "", "", l.Errorf("Attachment storage is not configured or the attachment ID is invalid", "附件存储尚未配置或附件标识无效")
	}
	base := filepath.Join(s.cfg.UploadDir, sessionID, uploadID)
	return base, base + ".json", nil
}

// sessionUploadDir returns the per-session upload directory, refusing anything
// that does not resolve to a direct child of UploadDir.
func (s *Server) sessionUploadDir(sessionID string) (string, error) {
	if !channelUUID.MatchString(sessionID) || !filepath.IsAbs(s.cfg.UploadDir) {
		return "", errors.New("invalid upload directory")
	}
	root := filepath.Clean(s.cfg.UploadDir)
	dir := filepath.Join(root, sessionID)
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel != sessionID || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errors.New("upload directory outside storage root")
	}
	return dir, nil
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := s.store.Get(sessionID); err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	id := store.ID()
	path, metaPath, err := s.uploadPaths(s.lang(r), sessionID, id)
	if err != nil {
		fail(w, 503, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+(1<<20))
	defer r.Body.Close()
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		fail(w, 400, s.lang(r).T("Invalid upload or larger than 20 MiB", "上传无效或超过 20 MiB"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 {
		fail(w, 400, s.lang(r).T("Choose one file at a time", "每次请选择一个文件"))
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail(w, 400, s.lang(r).T("Could not read the file", "无法读取文件"))
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	if err != nil || len(b) > maxUploadBytes {
		fail(w, 400, s.lang(r).T("A file cannot be larger than 20 MiB", "单个文件不能超过 20 MiB"))
		return
	}
	name := sanitizeUploadName(files[0].Filename)
	a := attachment{ID: id, Kind: "file", Name: name, Size: len(b), URL: fmt.Sprintf("/api/sessions/%s/uploads/%s", sessionID, id)}
	sniffed := http.DetectContentType(b)
	if format := imageMIMEs[sniffed]; format != "" {
		cfg, decoded, err := image.DecodeConfig(bytes.NewReader(b))
		if err != nil || decoded != format || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 25_000_000 {
			fail(w, 400, s.lang(r).T("Invalid image: only valid PNG, JPEG and GIF images up to 25 megapixels are supported", "图片无效：仅支持有效的 PNG、JPEG、GIF，最多 2500 万像素"))
			return
		}
		if _, _, err = image.Decode(bytes.NewReader(b)); err != nil {
			fail(w, 400, s.lang(r).T("The image is corrupt", "图片内容损坏"))
			return
		}
		a.Kind, a.MIME = "image", sniffed
		path += "." + format
	} else {
		a.MIME = sniffed
		if byExt := mime.TypeByExtension(filepath.Ext(name)); byExt != "" {
			a.MIME = byExt
		}
		if m, _, err := mime.ParseMediaType(a.MIME); err == nil {
			a.MIME = m
		}
		path += "-" + name
	}
	meta, _ := json.Marshal(a)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		err = os.WriteFile(path, b, 0600)
	}
	if err == nil {
		err = os.WriteFile(metaPath, meta, 0600)
	}
	if err != nil {
		_ = os.Remove(path)
		_ = os.Remove(metaPath)
		fail(w, 500, s.lang(r).T("Could not save the file", "无法保存文件"))
		return
	}
	respond(w, 201, a)
}

func (s *Server) loadAttachment(l i18n.Lang, sessionID, id string) (attachment, string, error) {
	path, metaPath, err := s.uploadPaths(l, sessionID, id)
	if err != nil {
		return attachment{}, "", err
	}
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return attachment{}, "", l.Errorf("The attachment does not exist or does not belong to this session", "附件不存在或不属于此会话")
	}
	var a attachment
	if json.Unmarshal(b, &a) != nil || a.ID != id {
		return a, "", l.Errorf("Invalid attachment record", "附件记录无效")
	}
	switch a.Kind {
	case "", "image":
		ext := imageMIMEs[a.MIME]
		if ext == "" {
			return a, "", l.Errorf("Invalid image format", "图片格式无效")
		}
		a.Kind = "image"
		path += "." + ext
	case "file":
		if a.Name != sanitizeUploadName(a.Name) {
			return a, "", l.Errorf("Invalid attachment name", "附件名称无效")
		}
		path += "-" + a.Name
	default:
		return a, "", l.Errorf("Invalid attachment kind", "附件类型无效")
	}
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return a, "", l.Errorf("Attachment not found", "附件不存在")
	}
	return a, path, nil
}

// inlineSafe lists the only types served inline; everything else downloads.
func inlineSafe(a attachment) bool { return a.Kind == "image" && imageMIMEs[a.MIME] != "" }

func (s *Server) uploadGet(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Get(r.PathValue("id")); err != nil {
		fail(w, 404, s.lang(r).T("Session not found", "会话不存在"))
		return
	}
	a, path, err := s.loadAttachment(s.lang(r), r.PathValue("id"), r.PathValue("uploadID"))
	if err != nil {
		fail(w, 404, s.lang(r).T("Attachment not found", "附件不存在"))
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if inlineSafe(a) {
		w.Header().Set("Content-Type", a.MIME)
		w.Header().Set("Content-Disposition", "inline")
	} else {
		contentType := a.MIME
		if m := strings.ToLower(contentType); m == "" || strings.Contains(m, "html") || strings.Contains(m, "xml") || strings.Contains(m, "svg") || strings.Contains(m, "script") {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Name}))
	}
	http.ServeFile(w, r, path)
}

// Messages carry text. Claude's native Read tool opens the validated
// host paths (text, PDF, images, notebooks) with normal permissions.
func (s *Server) messageAttachments(l i18n.Lang, sessionID, text string, ids []string) (string, json.RawMessage, error) {
	if len(ids) == 0 {
		return text, nil, nil
	}
	if len(ids) > maxAttachments {
		return "", nil, l.Errorf("A message can carry at most %d attachments", "每条消息最多附带 %d 个附件", maxAttachments)
	}
	items := make([]attachment, 0, len(ids))
	lines := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return "", nil, l.Errorf("The same file cannot be attached twice", "不能重复附加同一个文件")
		}
		seen[id] = true
		a, path, err := s.loadAttachment(l, sessionID, id)
		if err != nil {
			return "", nil, err
		}
		items = append(items, a)
		line, _ := json.Marshal(map[string]any{"path": path, "name": a.Name, "mime": a.MIME, "size": a.Size})
		lines = append(lines, string(line))
	}
	payload, _ := json.Marshal(map[string]any{"attachments": items})
	prompt := text + "\n\nThe user attached files stored on this host. Use the Read tool to open each attachment before answering (Read supports text, PDF, images and Jupyter notebooks); for other binary formats, inspect them with suitable tools. Attachments (one JSON object per line; path is the absolute host path, name is the original file name):\n" + strings.Join(lines, "\n")
	return prompt, payload, nil
}
