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
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cc-remote/internal/store"
)

const maxImageBytes = 10 << 20

type attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

func (s *Server) uploadPaths(sessionID, uploadID string) (string, string, error) {
	if !channelUUID.MatchString(sessionID) || !channelUUID.MatchString(uploadID) || !filepath.IsAbs(s.cfg.UploadDir) {
		return "", "", errors.New("图片存储尚未配置或图片标识无效")
	}
	base := filepath.Join(s.cfg.UploadDir, sessionID, uploadID)
	return base, base + ".json", nil
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if _, err := s.store.Get(sessionID); err != nil {
		fail(w, 404, "会话不存在")
		return
	}
	id := store.ID()
	path, metaPath, err := s.uploadPaths(sessionID, id)
	if err != nil {
		fail(w, 503, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+(1<<20))
	defer r.Body.Close()
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		fail(w, 400, "图片上传无效或超过 10 MiB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 {
		fail(w, 400, "每次请选择一张图片")
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail(w, 400, "无法读取图片")
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxImageBytes+1))
	if err != nil || len(b) > maxImageBytes {
		fail(w, 400, "图片超过 10 MiB")
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 25_000_000 {
		fail(w, 400, "仅支持有效的 PNG、JPEG、GIF 图片，最多 2500 万像素")
		return
	}
	if _, _, err = image.Decode(bytes.NewReader(b)); err != nil {
		fail(w, 400, "图片内容损坏")
		return
	}
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}[format]
	if mime == "" {
		fail(w, 400, "不支持的图片格式")
		return
	}
	path += "." + format
	name := filepath.Base(strings.ReplaceAll(files[0].Filename, "\\", "/"))
	if len(name) > 200 {
		name = "image"
	}
	a := attachment{ID: id, Name: name, MIME: mime, Size: len(b), URL: fmt.Sprintf("/api/sessions/%s/uploads/%s", sessionID, id)}
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
		fail(w, 500, "无法保存图片")
		return
	}
	respond(w, 201, a)
}

func (s *Server) loadAttachment(sessionID, id string) (attachment, string, error) {
	path, metaPath, err := s.uploadPaths(sessionID, id)
	if err != nil {
		return attachment{}, "", err
	}
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return attachment{}, "", errors.New("图片不存在或不属于此会话")
	}
	var a attachment
	if json.Unmarshal(b, &a) != nil || a.ID != id {
		return a, "", errors.New("图片记录无效")
	}
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpeg", "image/gif": "gif"}[a.MIME]
	if ext == "" {
		return a, "", errors.New("图片格式无效")
	}
	path += "." + ext
	if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
		return a, "", errors.New("图片不存在")
	}
	return a, path, nil
}

func (s *Server) uploadGet(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Get(r.PathValue("id")); err != nil {
		fail(w, 404, "会话不存在")
		return
	}
	a, path, err := s.loadAttachment(r.PathValue("id"), r.PathValue("uploadID"))
	if err != nil {
		fail(w, 404, "图片不存在")
		return
	}
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("Content-Disposition", "inline")
	http.ServeFile(w, r, path)
}

// Channel transports carry text. Claude's native Read tool opens the validated
// host image path and supplies its pixels to the model, with normal permissions.
func (s *Server) messageAttachments(sessionID, text string, ids []string) (string, json.RawMessage, error) {
	if len(ids) == 0 {
		return text, nil, nil
	}
	if len(ids) > 8 {
		return "", nil, errors.New("每条消息最多附带 8 张图片")
	}
	items := make([]attachment, 0, len(ids))
	paths := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return "", nil, errors.New("不能重复附加同一张图片")
		}
		seen[id] = true
		a, path, err := s.loadAttachment(sessionID, id)
		if err != nil {
			return "", nil, err
		}
		items = append(items, a)
		p, _ := json.Marshal(path)
		paths = append(paths, string(p))
	}
	payload, _ := json.Marshal(map[string]any{"attachments": items})
	prompt := text + "\n\nThe user attached images stored on this host. Use the Read tool to view each image before answering. Image paths (JSON strings):\n" + strings.Join(paths, "\n")
	return prompt, payload, nil
}
