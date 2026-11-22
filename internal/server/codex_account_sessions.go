package server

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"cc-remote/internal/claude"
	"cc-remote/internal/codex"
	"cc-remote/internal/i18n"
	"cc-remote/internal/store"
)

func isCodexAccountID(id string) bool {
	raw, ok := strings.CutPrefix(id, codexAccountIDPrefix)
	return ok && channelUUID.MatchString(raw)
}

func codexAccountProfile(account codexAccount) supplierProfile {
	p := supplierProfile{ID: account.ID, Name: account.Name, App: "codex"}
	p.Settings.Kind = oauthKind
	return p
}

func (s *Server) activeCodexAccountProfile(id string) (supplierProfile, error) {
	accounts, err := s.loadCodexAccounts()
	if err != nil {
		return supplierProfile{}, err
	}
	for _, account := range accounts {
		if account.ID == id && !account.Disabled {
			if dir, err := os.Stat(s.codexAccountHome(id)); err == nil && dir.IsDir() {
				return codexAccountProfile(account), nil
			}
			break
		}
	}
	return supplierProfile{}, i18n.Errorf("The original Codex account is unavailable", "原 Codex 账号已不可用")
}

func checkCodexAccountSwitch(session store.Session, supplier string, starting bool) error {
	if session.Provider == "codex" && (session.Resume || starting) && session.ProfileID != supplier &&
		(isCodexAccountID(session.ProfileID) || isCodexAccountID(supplier)) {
		return i18n.Errorf("This Codex thread belongs to its original account. Start a new session to choose another supplier", "此 Codex 对话属于原账号，请新建会话选择其他供应商")
	}
	return nil
}

type codexNativeHome struct {
	dir, profileID string
}

func (s *Server) codexNativeHomes() ([]codexNativeHome, error) {
	accounts, err := s.loadCodexAccounts()
	if err != nil {
		return nil, err
	}
	homes := []codexNativeHome{{dir: s.cfg.CodexConfigDir}}
	for _, account := range accounts {
		if dir := s.codexAccountHome(account.ID); dir != "" {
			homes = append(homes, codexNativeHome{dir: dir, profileID: account.ID})
		}
	}
	return homes, nil
}

func (s *Server) codexNativeHomeForThread(ctx context.Context, id string) (codexNativeHome, error) {
	homes, err := s.codexNativeHomes()
	if err != nil {
		return codexNativeHome{}, err
	}
	var found *codexNativeHome
	for _, home := range homes {
		_, err := codex.NativeMetadata(ctx, home.dir, id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return codexNativeHome{}, err
		}
		if found != nil {
			return codexNativeHome{}, i18n.Errorf("This Codex thread ID exists in more than one home", "这个 Codex 对话 ID 出现在多个账号目录中")
		}
		found = &home
	}
	if found == nil {
		return codexNativeHome{}, os.ErrNotExist
	}
	return *found, nil
}

func (s *Server) codexNativeHistory(ctx context.Context, id string) (claude.NativeSession, []claude.Event, error) {
	home, err := s.codexNativeHomeForThread(ctx, id)
	if err != nil {
		return claude.NativeSession{}, nil, err
	}
	session, events, err := codex.NativeHistory(home.dir, id)
	session.ProfileID = home.profileID
	return session, events, err
}

func (s *Server) listCodexNative() ([]claude.NativeSession, error) {
	homes, err := s.codexNativeHomes()
	if err != nil {
		return nil, err
	}
	sessions := []claude.NativeSession{}
	counts := map[string]int{}
	for _, home := range homes {
		items, err := codex.ListNative(home.dir)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			counts[strings.ToLower(item.ID)]++
			item.ProfileID = home.profileID
			sessions = append(sessions, item)
		}
	}
	sessions = slices.DeleteFunc(sessions, func(item claude.NativeSession) bool { return counts[strings.ToLower(item.ID)] > 1 })
	threadTime := func(n claude.NativeSession) time.Time {
		stamp := n.LastConversationAt
		if stamp == "" {
			stamp = n.CreatedAt
		}
		parsed, _ := time.Parse(time.RFC3339Nano, stamp)
		return parsed
	}
	sort.Slice(sessions, func(i, j int) bool {
		a, b := threadTime(sessions[i]), threadTime(sessions[j])
		if a.Equal(b) {
			return sessions[i].ID < sessions[j].ID
		}
		return a.After(b)
	})
	return sessions, nil
}

func (s *Server) nativeImportProfile(provider, accountID string, requested *string) (*supplierProfile, error) {
	if accountID != "" {
		if id := profileValue(requested); id != "" && id != accountID {
			return nil, i18n.Errorf("Import this Codex thread with its original account", "请使用原账号导入这个 Codex 对话")
		}
		p, err := s.activeCodexAccountProfile(accountID)
		return &p, err
	}
	p, err := s.resumeProfile(provider, requested)
	if err == nil && p != nil && isCodexAccountID(p.ID) {
		return nil, i18n.Errorf("This Codex thread belongs to the host configuration", "该 Codex 对话位于宿主原生目录")
	}
	return p, err
}
