package capture

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Capture struct {
	Dir      string
	Meta     Meta
	Segments []int
}

// Load opens a capture for reading.
func Load(dir string) (*Capture, error) {
	meta, err := readMeta(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", dir, errNotCapture)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	segs, err := listSegments(dir)
	if err != nil {
		return nil, err
	}
	return &Capture{Dir: dir, Meta: meta, Segments: segs}, nil
}

func listSegments(dir string) ([]int, error) {
	entries, err := os.ReadDir(filepath.Join(dir, segmentsDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []int
	for _, e := range entries {
		n, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".jsonl"))
		if err != nil || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

// Records reads a whole segment. a segment cut short by a crash reads up to
// its last whole line.
func (c *Capture) Records(segment int) ([]Record, error) {
	f, err := os.Open(segmentPath(c.Dir, segment))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Record
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var rec Record
			if jerr := json.Unmarshal(line, &rec); jerr != nil {
				return out, fmt.Errorf("segment %d record %d: %w", segment, len(out)+1, jerr)
			}
			out = append(out, rec)
		}
		if err != nil {
			// a torn last line is what a kill leaves, not an error
			return out, nil
		}
	}
}

func (c *Capture) BlobPath(name string) string {
	return filepath.Join(c.Dir, blobsDir, name)
}

func (c *Capture) Blob(name string) ([]byte, error) {
	if name == "" || strings.ContainsAny(name, `/\.`) {
		return nil, fmt.Errorf("bad blob name %q", name)
	}
	return os.ReadFile(c.BlobPath(name))
}

// Account is the newest account record up to and including segment, the
// identity a replay has to log in as.
func (c *Capture) Account(segment int) (Account, error) {
	var acct Account
	found := false
	for _, n := range c.Segments {
		if n > segment {
			break
		}
		recs, err := c.Records(n)
		if err != nil {
			return acct, err
		}
		for _, r := range recs {
			if r.Kind == KindAccount && r.Account != nil && r.Account.PN != "" {
				acct, found = *r.Account, true
			}
		}
	}
	if !found {
		return acct, fmt.Errorf("%s has no account record up to segment %d", c.Dir, segment)
	}
	return acct, nil
}
