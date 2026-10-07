package frontends

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/codelif/whatevr/platform"
)

// defaultTerminal hands Terminal.app a .command file. terminal runs it in
// the user's shell with their PATH, and opening a file needs no automation
// permission the way scripting terminal would.
func defaultTerminal(f Frontend) ([]string, error) {
	dir, err := platform.RuntimeDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "whatevr")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	quoted := make([]string, len(f.Exec))
	for i, a := range f.Exec {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	p := filepath.Join(dir, "launch-"+f.ID+".command")
	script := "#!/bin/sh\nexec " + strings.Join(quoted, " ") + "\n"
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		return nil, err
	}
	// WriteFile keeps the mode of a file that was already there
	if err := os.Chmod(p, 0o700); err != nil {
		return nil, err
	}
	return []string{"/usr/bin/open", "-a", "Terminal", p}, nil
}
