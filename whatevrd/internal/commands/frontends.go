package commands

import (
	"context"
	"sort"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/frontends"
	"whatevrd/internal/server"
)

var sources = map[frontends.Source]v2.FrontendSource{
	frontends.System: v2.FrontendSource_FRONTEND_SOURCE_SYSTEM,
	frontends.Native: v2.FrontendSource_FRONTEND_SOURCE_NATIVE,
	frontends.User:   v2.FrontendSource_FRONTEND_SOURCE_USER,
}

func (x *commands) frontendList(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	def := frontends.DefaultID(x.c.Prefs(ctx).GetDefaultFrontend())
	connected := map[string]bool{}
	for _, st := range x.srv.Sessions() {
		if st.Frontend != "" {
			connected[st.Frontend] = true
		}
	}
	var rows []*v2.Frontend
	known := map[string]bool{}
	for _, f := range frontends.List(x.dirs) {
		known[f.ID] = true
		name := f.Name
		if name == "" {
			name = f.ID
		}
		rows = append(rows, v2.Frontend_builder{Id: f.ID, Name: name, Terminal: f.Terminal, Source: sources[f.Source],
			Connected: connected[f.ID], IsDefault: f.ID == def}.Build())
	}
	// connected but not startable: still worth showing in a picker
	for id := range connected {
		if !known[id] && frontends.ValidID(id) {
			rows = append(rows, v2.Frontend_builder{Id: id, Name: id, Connected: true, IsDefault: id == def}.Build())
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].GetId() < rows[j].GetId() })
	resp := &v2.Response{}
	resp.SetFrontendList(v2.FrontendListResult_builder{Frontends: rows}.Build())
	return resp, nil
}

func (x *commands) frontendSetDefault(ctx context.Context, s *server.Session, req *v2.Request) (*v2.Response, error) {
	id := req.GetFrontendSetDefault().GetId()
	if !frontends.ValidID(id) {
		return nil, invalid("bad frontend id %q", id)
	}
	if _, ok := frontends.Find(x.dirs, id); !ok {
		return nil, notFound("no frontend %q the daemon can start", id)
	}
	return nil, wire(x.c.SetDefaultFrontend(ctx, id))
}
