package server

import "net/http"

func (s *Server) supplierSwitchWarning(w http.ResponseWriter, r *http.Request) {
	l := s.lang(r)
	if r.Method == http.MethodPut {
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if !decode(w, r, &req) {
			return
		}
		if req.Enabled == nil {
			fail(w, http.StatusBadRequest, l.T("Specify whether to warn before switching suppliers", "请指定是否显示供应商切换提示"))
			return
		}
		if err := s.store.SetSupplierSwitchWarning(*req.Enabled); err != nil {
			fail(w, http.StatusInternalServerError, l.T("Could not save the supplier switch warning setting", "无法保存供应商切换提示设置"))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	enabled, err := s.store.SupplierSwitchWarning()
	if err != nil {
		fail(w, http.StatusInternalServerError, l.T("Could not read the supplier switch warning setting", "无法读取供应商切换提示设置"))
		return
	}
	respond(w, http.StatusOK, struct {
		Enabled bool `json:"enabled"`
	}{enabled})
}
