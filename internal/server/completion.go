package server

import (
	"net/http"
	"time"
)

func (s *Server) markCompletionRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CompletedAt string `json:"completedAt"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, err := time.Parse(time.RFC3339Nano, req.CompletedAt); err != nil {
		fail(w, http.StatusBadRequest, s.lang(r).T("Invalid completion identity", "无效的完成标识"))
		return
	}
	if err := s.store.MarkCompletionRead(r.PathValue("id"), req.CompletedAt); err != nil {
		fail(w, http.StatusInternalServerError, s.lang(r).T("Could not mark the completion as read", "无法将完成结果标记为已读"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
