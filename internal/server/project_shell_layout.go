package server

import (
	"net/http"
	"strings"
)

func (m *projectShellRegistry) openTab(project, tabID string) []*projectShellEntry {
	var members []*projectShellEntry
	for _, e := range m.entries {
		if e.ProjectKey == project && e.tabID() == tabID && isOpenProjectShell(e.state()) {
			members = append(members, e)
		}
	}
	return members
}

func splitSides(direction string) (first, second string) {
	if direction == "rows" {
		return "U", "D"
	}
	return "L", "R"
}

// Paths encode the side of each split from the root. Compacting discards
// ancestors whose other branch is empty, including after a terminal exits.
func projectShellPanePaths(members []*projectShellEntry) map[string]string {
	paths := make(map[string]string, len(members))
	for _, e := range members {
		paths[e.ID] = e.PanePath
	}
	if len(members) == 2 && members[0].PanePath == "" && members[1].PanePath == "" {
		first, second := members[0], members[1]
		if first.SplitDirection != "" {
			first, second = second, first
		}
		paths[first.ID], paths[second.ID] = splitSides(second.SplitDirection)
	}
	compacted := make(map[string]string, len(paths))
	compactPanePaths(paths, compacted, "", "")
	return compacted
}

func compactPanePaths(paths, compacted map[string]string, old, next string) {
	children := make(map[byte]bool, 2)
	for id, path := range paths {
		if path == old {
			compacted[id] = next
			return
		}
		if strings.HasPrefix(path, old) {
			children[path[len(old)]] = true
		}
	}
	for side := range children {
		oldChild := old + string(side)
		if len(children) == 1 {
			compactPanePaths(paths, compacted, oldChild, next)
		} else {
			compactPanePaths(paths, compacted, oldChild, next+string(side))
		}
	}
}

func applyProjectShellPaths(members []*projectShellEntry, paths map[string]string) func() {
	previous := make([]projectShellRecord, len(members))
	for i, e := range members {
		previous[i] = e.projectShellRecord
		e.PanePath = paths[e.ID]
		e.SplitDirection = ""
		// Keep the two-pane metadata understood by older clients.
		if e.PanePath == "R" {
			e.SplitDirection = "columns"
		} else if e.PanePath == "D" {
			e.SplitDirection = "rows"
		}
	}
	return func() {
		for i, e := range members {
			e.projectShellRecord = previous[i]
		}
	}
}

// Called with the registry lock held. Rotate the nearest split containing the pane.
func (s *Server) reorientProjectShell(w http.ResponseWriter, r *http.Request, entry *projectShellEntry, direction string) {
	m := &s.projectShells
	members := m.openTab(entry.ProjectKey, entry.tabID())
	paths := projectShellPanePaths(members)
	path := paths[entry.ID]
	if path == "" {
		l := s.lang(r)
		fail(w, http.StatusConflict, l.T("This terminal has no split to change", "此终端没有可调整方向的分屏"))
		return
	}
	parent := path[:len(path)-1]
	first, second := splitSides(direction)
	for id, candidate := range paths {
		if len(candidate) <= len(parent) || !strings.HasPrefix(candidate, parent) {
			continue
		}
		side := second
		if strings.ContainsRune("LU", rune(candidate[len(parent)])) {
			side = first
		}
		paths[id] = parent + side + candidate[len(parent)+1:]
	}
	rollback := applyProjectShellPaths(members, paths)
	if err := m.saveLocked(s.cfg.DataDir, ""); err != nil {
		rollback()
		s.projectShellStorageError(w, r, err)
		return
	}
	view := entry.view()
	view.PanePaths = paths
	respond(w, http.StatusOK, view)
}
