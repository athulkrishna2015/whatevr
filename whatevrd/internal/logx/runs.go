package logx

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const runTimeFormat = time.RFC3339

// whatevrd-<start>-<id>.jsonl is the live part, lumberjack renames full parts
// to whatevrd-<start>-<id>-<rotated at>.jsonl and gzips them.
var runFileRE = regexp.MustCompile(`^whatevrd-(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2}))-([0-9a-f]{8})(?:-([0-9T:.-]+))?\.jsonl(?:\.gz)?$`)

type RunFiles struct {
	ID    string
	Start time.Time
	// Parts oldest first, the live part last.
	Parts []string
}

func runFilePath(dir string, start time.Time, id string) string {
	return filepath.Join(dir, "whatevrd-"+start.Format(runTimeFormat)+"-"+id+".jsonl")
}

// ListRuns returns the runs in dir oldest first. files that don't look like
// ours are left out.
func ListRuns(dir string) ([]RunFiles, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type part struct {
		path    string
		rotated string
	}
	byID := map[string]*RunFiles{}
	parts := map[string][]part{}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// a part mid gzip exists twice, the plain one is whole
		if plain, ok := strings.CutSuffix(e.Name(), ".gz"); ok && names[plain] {
			continue
		}
		m := runFileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		start, err := time.Parse(runTimeFormat, m[1])
		if err != nil {
			continue
		}
		id := m[2]
		if byID[id] == nil {
			byID[id] = &RunFiles{ID: id, Start: start}
		}
		// the live part has no rotation stamp, "~" sorts it after every stamp
		rotated := m[3]
		if rotated == "" {
			rotated = "~"
		}
		parts[id] = append(parts[id], part{filepath.Join(dir, e.Name()), rotated})
	}
	runs := make([]RunFiles, 0, len(byID))
	for id, r := range byID {
		ps := parts[id]
		sort.Slice(ps, func(i, j int) bool { return ps[i].rotated < ps[j].rotated })
		for _, p := range ps {
			r.Parts = append(r.Parts, p.path)
		}
		runs = append(runs, *r)
	}
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].Start.Equal(runs[j].Start) {
			return runs[i].Start.Before(runs[j].Start)
		}
		return runs[i].ID < runs[j].ID
	})
	return runs, nil
}

// FindRun picks a run by id prefix, empty means the newest.
func FindRun(runs []RunFiles, want string) (RunFiles, bool) {
	if len(runs) == 0 {
		return RunFiles{}, false
	}
	if want == "" {
		return runs[len(runs)-1], true
	}
	var found []RunFiles
	for _, r := range runs {
		if strings.HasPrefix(r.ID, want) {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		return RunFiles{}, false
	}
	return found[0], true
}

// pruneRuns deletes every part of all but the newest keep runs.
func pruneRuns(dir string, keep int) error {
	runs, err := ListRuns(dir)
	if err != nil {
		return err
	}
	for i := 0; i < len(runs)-keep; i++ {
		for _, p := range runs[i].Parts {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
