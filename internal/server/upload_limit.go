package server

import "net/http"

type uploadLimitSetting struct {
	LimitMiB int `json:"limitMiB"`
	MaxMiB   int `json:"maxMiB"`
}

func (s *Server) uploadLimit(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	if r.Method == http.MethodPut {
		var req uploadLimitSetting
		if !decode(w, r, &req) {
			return
		}
		if req.LimitMiB < 1 || req.LimitMiB > maxUploadMiB {
			fail(w, http.StatusBadRequest, l.F("Enter a whole number between 1 and %d MiB", "请输入 1 到 %d MiB 之间的整数", maxUploadMiB))
			return
		}
		if err := s.store.SetUploadLimitMiB(req.LimitMiB); err != nil {
			fail(w, http.StatusInternalServerError, l.T("Could not save the upload limit", "无法保存上传上限"))
			return
		}
		respond(w, http.StatusOK, uploadLimitSetting{req.LimitMiB, maxUploadMiB})
		return
	}
	limit, err := s.store.UploadLimitMiB(defaultUploadMiB)
	if err != nil {
		fail(w, http.StatusInternalServerError, l.T("Could not read the upload limit", "无法读取上传上限"))
		return
	}
	respond(w, http.StatusOK, uploadLimitSetting{limit, maxUploadMiB})
}
