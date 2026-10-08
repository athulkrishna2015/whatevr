package views

import (
	"context"
	"strconv"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/server"
)

// foldersView lists the user's chat lists, by name like the sidebar shows
// them. Folder mutations touch "folders", so open windows re-read.
func (rs *Reads) foldersView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		folders, err := rs.r.Folders(ctx)
		if err != nil {
			return nil, err
		}
		var out []*v2.Upsert
		for _, f := range folders {
			row := v2.ChatFolderRow_builder{Id: f.ID, Name: f.Name}.Build()
			it := &v2.Upsert{}
			it.SetId(strconv.FormatInt(f.ID, 10))
			it.SetSort([]byte(f.Name + "\x00" + strconv.FormatInt(f.ID, 10)))
			it.SetChatFolder(row)
			out = append(out, it)
		}
		return limited(out, max), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "folders") }
	return w, nil, nil
}
