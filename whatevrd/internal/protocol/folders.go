package protocol

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync"

	"whatevrd/internal/store"
)

type FolderLister interface {
	ListChatFolders(context.Context) ([]store.ChatFolder, error)
}

// foldersView tracks its open sessions so the chat_folder.* commands can kick
// them: folders have no daemon event, so without this a create, rename or
// delete never reaches an open chat_folders subscription.
type foldersView struct {
	lister FolderLister

	mu       sync.Mutex
	sessions map[*folderSession]struct{}
}

func (v *foldersView) Open(_ json.RawMessage, invalidate func()) (ViewSession, map[string]any, *Error) {
	s := &folderSession{lister: v.lister, invalidate: invalidate, view: v}
	v.mu.Lock()
	if v.sessions == nil {
		v.sessions = make(map[*folderSession]struct{})
	}
	v.sessions[s] = struct{}{}
	v.mu.Unlock()
	return s, nil, nil
}

// invalidateAll schedules a recomputation on every open chat_folders window.
func (v *foldersView) invalidateAll() {
	v.mu.Lock()
	sessions := make([]*folderSession, 0, len(v.sessions))
	for s := range v.sessions {
		sessions = append(sessions, s)
	}
	v.mu.Unlock()
	for _, s := range sessions {
		s.invalidate()
	}
}

type folderSession struct {
	lister     FolderLister
	invalidate func()
	view       *foldersView
}

func (s *folderSession) Items(_ int) []Item {
	items, _ := s.ItemsErr(0)
	return items
}

func (s *folderSession) ItemsErr(_ int) ([]Item, error) {
	if s.lister == nil {
		return nil, nil
	}
	folders, err := s.lister.ListChatFolders(context.Background())
	if err != nil {
		log.Printf("protocol: list chat folders for view: %v", err)
		return nil, err
	}
	items := make([]Item, 0, len(folders))
	for _, f := range folders {
		id := strconv.FormatInt(f.ID, 10)
		items = append(items, Item{ID: id, Sort: f.Name, Data: map[string]any{"id": id, "folder_id": f.ID, "name": f.Name}})
	}
	return items, nil
}

func (s *folderSession) Close() {
	s.view.mu.Lock()
	delete(s.view.sessions, s)
	s.view.mu.Unlock()
}

type folderActions interface {
	CreateChatFolder(context.Context, string) (store.ChatFolder, error)
	RenameChatFolder(context.Context, int64, string) error
	DeleteChatFolder(context.Context, int64) error
	SetChatFolder(context.Context, string, *int64) error
}

// invalidateFolders kicks every open chat_folders window. The folder mutations
// run as commands with no daemon event behind them, so they notify the view
// themselves.
func (h commandHandlers) invalidateFolders() {
	v, ok := h.server.lookupView("chat_folders")
	if !ok {
		return
	}
	if fv, ok := v.(*foldersView); ok {
		fv.invalidateAll()
	}
}

type folderCreateParams struct {
	Name string `json:"name"`
}

func (h commandHandlers) folderCreate(ctx context.Context, _ *conn, req request) (any, *Error) {
	a, ok := h.actions.(folderActions)
	if !ok {
		return nil, errorf(CodeInternal, "folder actions are not available")
	}
	var p folderCreateParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	f, err := a.CreateChatFolder(ctx, p.Name)
	if err != nil {
		return nil, mapCommandError(err)
	}
	h.invalidateFolders()
	return map[string]any{"id": f.ID, "name": f.Name}, nil
}

type folderRenameParams struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (h commandHandlers) folderRename(ctx context.Context, _ *conn, req request) (any, *Error) {
	a, ok := h.actions.(folderActions)
	if !ok {
		return nil, errorf(CodeInternal, "folder actions are not available")
	}
	var p folderRenameParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	if err := a.RenameChatFolder(ctx, p.ID, p.Name); err != nil {
		return nil, mapCommandError(err)
	}
	h.invalidateFolders()
	return nil, nil
}

type folderDeleteParams struct {
	ID int64 `json:"id"`
}

func (h commandHandlers) folderDelete(ctx context.Context, _ *conn, req request) (any, *Error) {
	a, ok := h.actions.(folderActions)
	if !ok {
		return nil, errorf(CodeInternal, "folder actions are not available")
	}
	var p folderDeleteParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	if err := a.DeleteChatFolder(ctx, p.ID); err != nil {
		return nil, mapCommandError(err)
	}
	h.invalidateFolders()
	return nil, nil
}

type folderSetChatParams struct {
	ChatID   string `json:"chat_id"`
	FolderID *int64 `json:"folder_id"`
}

func (h commandHandlers) folderSetChat(ctx context.Context, _ *conn, req request) (any, *Error) {
	a, ok := h.actions.(folderActions)
	if !ok {
		return nil, errorf(CodeInternal, "folder actions are not available")
	}
	var p folderSetChatParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.ChatID) == "" {
		return nil, errorf(CodeInvalidParams, "chat_id is required")
	}
	return nil, mapCommandError(a.SetChatFolder(ctx, p.ChatID, p.FolderID))
}
