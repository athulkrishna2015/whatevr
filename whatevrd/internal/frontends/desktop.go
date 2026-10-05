package frontends

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// desktopDirs is the xdg applications dirs, the one that wins first.
func desktopDirs() []string {
	var out []string
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	if filepath.IsAbs(home) {
		out = append(out, filepath.Join(home, "applications"))
	}
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	for _, p := range filepath.SplitList(dirs) {
		if filepath.IsAbs(p) {
			out = append(out, filepath.Join(p, "applications"))
		}
	}
	return out
}

// desktopFrontends is every .desktop entry with X-Whatevr-Frontend set.
func desktopFrontends(dirs []string) []Frontend {
	seen := map[string]bool{}
	var out []Frontend
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			// a desktop file id shadows the same id in a later dir
			if e.IsDir() || filepath.Ext(e.Name()) != ".desktop" || seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			p := filepath.Join(dir, e.Name())
			if f, err := readDesktop(p); err == nil {
				out = append(out, f)
			}
		}
	}
	return out
}

func readDesktop(p string) (Frontend, error) {
	file, err := os.Open(p)
	if err != nil {
		return Frontend{}, err
	}
	defer file.Close()
	keys := map[string]string{}
	group := ""
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			group = line
			continue
		}
		if group != "[Desktop Entry]" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if err := sc.Err(); err != nil {
		return Frontend{}, err
	}
	id := keys["X-Whatevr-Frontend"]
	if !ValidID(id) || keys["Type"] != "Application" || keys["Hidden"] == "true" {
		return Frontend{}, errors.New("not a frontend")
	}
	argv, err := desktopExec(keys["Exec"])
	if err != nil {
		return Frontend{}, err
	}
	return Frontend{ID: id, Name: keys["Name"], Exec: argv, Terminal: keys["Terminal"] == "true", Source: Native, From: p}, nil
}

// desktopExec splits an Exec value as the desktop entry spec quotes it, and
// drops the field codes: a frontend gets its chat over the protocol.
func desktopExec(v string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	in, quoted, escaped := false, false, false
	flush := func() {
		if in {
			argv = append(argv, cur.String())
		}
		cur.Reset()
		in = false
	}
	for _, r := range v {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quoted && r == '\\':
			escaped = true
		case r == '"':
			quoted = !quoted
			in = true
		case !quoted && (r == ' ' || r == '\t'):
			flush()
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if quoted || escaped {
		return nil, errors.New("unterminated quote in Exec")
	}
	flush()
	out := argv[:0]
	for _, a := range argv {
		if len(a) == 2 && a[0] == '%' {
			continue
		}
		out = append(out, strings.ReplaceAll(a, "%%", "%"))
	}
	if len(out) == 0 {
		return nil, errors.New("empty Exec")
	}
	return out, nil
}
