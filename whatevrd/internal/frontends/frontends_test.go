package frontends

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUserBeatsSystemAndFirstSystemDirWins(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", t.TempDir())
	root := t.TempDir()
	a, b, user := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "user")
	writeFile(t, filepath.Join(a, "whattui.json"), `{"id":"whattui","exec":["whattui"],"terminal":true}`)
	writeFile(t, filepath.Join(b, "whattui.json"), `{"id":"whattui","exec":["old"]}`)
	writeFile(t, filepath.Join(b, "other.json"), `{"id":"other","exec":["other"]}`)
	writeFile(t, filepath.Join(b, "broken.json"), `{"id":"Bad Id","exec":["x"]}`)
	writeFile(t, filepath.Join(user, "other.json"), `{"id":"other","name":"Mine","exec":["mine","--x"]}`)
	got := List(Dirs{System: []string{a, b}, User: user})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].ID != "other" || got[0].Source != User || !reflect.DeepEqual(got[0].Exec, []string{"mine", "--x"}) {
		t.Fatalf("other %+v", got[0])
	}
	if got[1].ID != "whattui" || got[1].Source != System || got[1].Exec[0] != "whattui" || !got[1].Terminal {
		t.Fatalf("whattui %+v", got[1])
	}
}

func TestWriteAndRemoveUserManifest(t *testing.T) {
	d := Dirs{User: filepath.Join(t.TempDir(), "frontends")}
	if _, err := Write(d, Manifest{ID: "x", Exec: nil}); err == nil {
		t.Fatal("wrote a manifest with nothing to run")
	}
	if _, err := Write(d, Manifest{ID: "../x", Exec: []string{"a"}}); err == nil {
		t.Fatal("wrote outside the dir")
	}
	if _, err := Write(d, Manifest{ID: "ghostty-tui", Exec: []string{"ghostty", "-e", "whattui"}}); err != nil {
		t.Fatal(err)
	}
	f, ok := Find(d, "ghostty-tui")
	if !ok || f.Source != User || len(f.Exec) != 3 {
		t.Fatalf("find %+v %v", f, ok)
	}
	if err := Remove(d, "ghostty-tui"); err != nil {
		t.Fatal(err)
	}
	if _, ok := Find(d, "ghostty-tui"); ok {
		t.Fatal("still there")
	}
}

func TestDesktopEntries(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(first, "a.desktop"), "[Desktop Entry]\nType=Application\nName=A\nExec=\"/opt/a b/run\" --open %u\nTerminal=true\nX-Whatevr-Frontend=a\n[Desktop Action x]\nX-Whatevr-Frontend=nope\n")
	// shadowed by first/a.desktop
	writeFile(t, filepath.Join(second, "a.desktop"), "[Desktop Entry]\nType=Application\nExec=old\nX-Whatevr-Frontend=old\n")
	writeFile(t, filepath.Join(second, "plain.desktop"), "[Desktop Entry]\nType=Application\nExec=plain\n")
	writeFile(t, filepath.Join(second, "hidden.desktop"), "[Desktop Entry]\nType=Application\nExec=h\nHidden=true\nX-Whatevr-Frontend=h\n")
	got := desktopFrontends([]string{first, second})
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	want := Frontend{ID: "a", Name: "A", Exec: []string{"/opt/a b/run", "--open"}, Terminal: true, Source: Native, From: filepath.Join(first, "a.desktop")}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("got %+v", got[0])
	}
}

func TestDesktopExec(t *testing.T) {
	for in, want := range map[string][]string{
		`whattui`:           {"whattui"},
		`foo %U --bar`:      {"foo", "--bar"},
		`"a \"q\" b" 100%%`: {`a "q" b`, "100%"},
		`  spaced   out  `:  {"spaced", "out"},
		`"" x`:              {"", "x"},
	} {
		got, err := desktopExec(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := desktopExec(`"open`); err == nil {
		t.Error("unterminated quote passed")
	}
}

func TestTerminalArgv(t *testing.T) {
	f := Frontend{ID: "whattui", Exec: []string{"whattui"}, Terminal: true}
	got, err := inTerminal(f, []string{"ghostty", "-e"})
	if err != nil || strings.Join(got, " ") != "ghostty -e whattui" {
		t.Fatalf("%q %v", got, err)
	}
}
