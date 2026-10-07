// Package frontends is every frontend the daemon can start: manifests a
// package shipped, the platform's own app entries, and manifests the user
// wrote, merged by id with the user's winning.
package frontends

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/codelif/whatevr/platform"
)

// Default is the frontend a fresh install starts.
const Default = "whattui"

// Source is where a frontend was found. later sources win.
type Source int

const (
	System Source = iota + 1
	Native
	User
)

func (s Source) String() string {
	switch s {
	case System:
		return "system"
	case Native:
		return "native"
	case User:
		return "user"
	}
	return "unknown"
}

type Frontend struct {
	ID   string
	Name string
	// Exec is the argv to run. empty for an app opened by its bundle
	Exec []string
	// App is a macOS bundle to open instead of running Exec
	App      string
	Terminal bool
	Source   Source
	// From is the file or bundle it was read from
	From string
}

// Manifest is a frontend as a json file describes it.
type Manifest struct {
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Exec     []string `json:"exec"`
	Terminal bool     `json:"terminal,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidID is whether id can name a frontend.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Dirs is where manifests are looked for.
type Dirs struct {
	System []string
	User   string
}

// DefaultDirs is the system dirs next to this executable and the platform's
// data dirs, and frontends under the user's config dir.
func DefaultDirs() Dirs {
	var d Dirs
	add := func(p string) {
		for _, have := range d.System {
			if have == p {
				return
			}
		}
		d.System = append(d.System, p)
	}
	if exe, err := os.Executable(); err == nil {
		// as invoked: a homebrew bin link points back at its own prefix
		add(filepath.Join(filepath.Dir(filepath.Dir(exe)), "share", "whatevr", "frontends"))
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			if app := bundleOf(real); app != "" {
				add(filepath.Join(app, "Contents", "Resources", "frontends"))
			}
			add(filepath.Join(filepath.Dir(filepath.Dir(real)), "share", "whatevr", "frontends"))
		}
	}
	if runtime.GOOS != "darwin" {
		dirs := os.Getenv("XDG_DATA_DIRS")
		if dirs == "" {
			dirs = "/usr/local/share:/usr/share"
		}
		for _, p := range filepath.SplitList(dirs) {
			if filepath.IsAbs(p) {
				add(filepath.Join(p, "whatevr", "frontends"))
			}
		}
	}
	if c, err := platform.ConfigDir(); err == nil {
		d.User = filepath.Join(c, "frontends")
	}
	return d
}

// bundleOf is the .app a binary in Contents/MacOS belongs to.
func bundleOf(exe string) string {
	dir := filepath.Dir(exe)
	contents := filepath.Dir(dir)
	app := filepath.Dir(contents)
	if filepath.Base(dir) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(app, ".app") {
		return app
	}
	return ""
}

// List is every frontend found, one per id, sorted by id.
func List(d Dirs) []Frontend {
	byID := map[string]Frontend{}
	put := func(f Frontend) {
		if have, ok := byID[f.ID]; ok && have.Source > f.Source {
			return
		}
		byID[f.ID] = f
	}
	// an earlier system dir wins over a later one, so walk them backwards
	for i := len(d.System) - 1; i >= 0; i-- {
		for _, f := range readDir(d.System[i], System) {
			put(f)
		}
	}
	for _, f := range native() {
		put(f)
	}
	if d.User != "" {
		for _, f := range readDir(d.User, User) {
			put(f)
		}
	}
	out := make([]Frontend, 0, len(byID))
	for _, f := range byID {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Find is the frontend id names, false when there is none.
func Find(d Dirs, id string) (Frontend, bool) {
	for _, f := range List(d) {
		if f.ID == id {
			return f, true
		}
	}
	return Frontend{}, false
}

func readDir(dir string, src Source) []Frontend {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Frontend
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		m, err := ReadManifest(p)
		if err != nil {
			continue
		}
		out = append(out, Frontend{ID: m.ID, Name: m.Name, Exec: m.Exec, Terminal: m.Terminal, Source: src, From: p})
	}
	return out
}

// ReadManifest reads and checks one manifest.
func ReadManifest(p string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(p)
	if err != nil {
		return m, err
	}
	if len(b) > 64<<10 {
		return m, fmt.Errorf("%s: manifest too big", p)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("%s: %w", p, err)
	}
	return m, m.check()
}

func (m Manifest) check() error {
	if !ValidID(m.ID) {
		return fmt.Errorf("bad frontend id %q", m.ID)
	}
	if len(m.Exec) == 0 || m.Exec[0] == "" {
		return errors.New("a manifest needs exec")
	}
	return nil
}

// Write saves m as the user's manifest for its id.
func Write(d Dirs, m Manifest) (string, error) {
	if err := m.check(); err != nil {
		return "", err
	}
	if d.User == "" {
		return "", errors.New("no user config dir")
	}
	if err := os.MkdirAll(d.User, 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	p := filepath.Join(d.User, m.ID+".json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

// Remove deletes the user's manifest for id.
func Remove(d Dirs, id string) error {
	if !ValidID(id) || d.User == "" {
		return fmt.Errorf("bad frontend id %q", id)
	}
	return os.Remove(filepath.Join(d.User, id+".json"))
}

// DefaultID is the stored default, whattui when nothing is stored.
func DefaultID(stored string) string {
	if stored == "" {
		return Default
	}
	return stored
}
