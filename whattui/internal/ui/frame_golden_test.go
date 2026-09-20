package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/term"
	"whattui/internal/theme"
)

func TestGoldenFrames(t *testing.T) {
	oldLocal := time.Local
	time.Local = time.FixedZone("golden", 0)
	t.Cleanup(func() { time.Local = oldLocal })

	sizes := []struct {
		name       string
		cols, rows int
	}{
		{name: "120x40", cols: 120, rows: 40},
		{name: "100x30", cols: 100, rows: 30},
		{name: "99x30", cols: 99, rows: 30},
		{name: "68x24", cols: 68, rows: 24},
		{name: "67x24", cols: 67, rows: 24},
		{name: "40x20", cols: 40, rows: 20},
		{name: "39x20", cols: 39, rows: 20},
		{name: "38x14", cols: 38, rows: 14},
	}
	for _, size := range sizes {
		t.Run(size.name, func(t *testing.T) {
			var golden strings.Builder
			var geometry layout.Layout
			for i, tier := range []term.Tier{term.TierPlain, term.TierColor, term.TierGraphics} {
				a := goldenApp(size.cols, size.rows, tier)
				a.paint()
				if tier == term.TierGraphics {
					addSyntheticPlacement(a, size.cols)
				}
				// The frame is staged by painting and placed by flushing, and
				// what a terminal would actually show needs both.
				a.flushImages()

				gotGeometry := a.layout()
				if i == 0 {
					geometry = gotGeometry
				} else if gotGeometry != geometry {
					t.Fatalf("tier %s geometry = %+v, plain geometry = %+v", tier, gotGeometry, geometry)
				}

				snapshot := a.vx.Snapshot()
				if links := snapshotLinks(snapshot); links == 0 {
					t.Fatalf("tier %s frame has no hyperlink cells", tier)
				}
				if cursor := snapshot.Cursor(); !cursor.Visible || cursor.Style != vaxis.CursorBeam {
					t.Fatalf("tier %s cursor = %#v, want visible beam", tier, cursor)
				}
				placements := snapshot.Placements()
				if tier == term.TierGraphics {
					want := vaxis.PlacementSnapshot{
						Column: size.cols - 2, Row: 1, Width: 1, Height: 1,
						ImageID: 1, XOffset: 2, YOffset: 3, ZIndex: -2,
					}
					if !hasPlacement(placements, want) {
						t.Fatalf("graphics placements = %#v, want one of them %#v", placements, want)
					}
					// Everything else on this frame is bubble chrome, which is
					// only ever drawn under the text.
					for _, p := range placements {
						if p != want && p.ZIndex != chromeZ {
							t.Fatalf("placement %#v is not under the text", p)
						}
					}
					if len(placements) < 2 {
						t.Fatal("the graphics tier drew no chrome")
					}
				} else if len(placements) != 0 {
					t.Fatalf("tier %s placements = %#v, want none", tier, placements)
				}

				fmt.Fprintf(&golden, "tier %s\nlayout %+v\n", tier, gotGeometry)
				writeSnapshot(&golden, snapshot)
			}
			assertGolden(t, filepath.Join("testdata", "frames_"+size.name+".golden"), golden.String())
		})
	}
}

func goldenApp(cols, rows int, tier term.Tier) *App {
	a := benchApp(cols, rows, 8, 5)
	a.caps = term.Caps{
		Tier:          tier,
		RGB:           tier >= term.TierColor,
		KittyGraphics: tier >= term.TierGraphics,
		Hyperlinks:    true,
		KittyKeyboard: true,
	}
	if tier == term.TierPlain {
		a.theme = theme.Default()
	}
	return a
}

func hasPlacement(placements []vaxis.PlacementSnapshot, want vaxis.PlacementSnapshot) bool {
	for _, p := range placements {
		if p == want {
			return true
		}
	}
	return false
}

func addSyntheticPlacement(a *App, cols int) {
	img := a.vx.NewKittyPixels(image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	img.SetOffset(2, 3)
	img.SetZIndex(-2)
	img.Draw(a.vx.Window().New(cols-2, 1, 1, 1))
}

func snapshotLinks(snapshot vaxis.FrameSnapshot) int {
	cols, rows := snapshot.Dimensions()
	n := 0
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			cell, _ := snapshot.Cell(col, row)
			if cell.Hyperlink != "" {
				n++
			}
		}
	}
	return n
}

type goldenCellKind struct {
	Width           int
	Size            vaxis.TextSize
	Foreground      vaxis.Color
	Background      vaxis.Color
	UnderlineColor  vaxis.Color
	UnderlineStyle  vaxis.UnderlineStyle
	Attribute       vaxis.AttributeMask
	Hyperlink       string
	HyperlinkParams string
}

func cellKind(cell vaxis.Cell) goldenCellKind {
	return goldenCellKind{
		Width:           cell.Width,
		Size:            cell.Size,
		Foreground:      cell.Foreground,
		Background:      cell.Background,
		UnderlineColor:  cell.UnderlineColor,
		UnderlineStyle:  cell.UnderlineStyle,
		Attribute:       cell.Attribute,
		Hyperlink:       cell.Hyperlink,
		HyperlinkParams: cell.HyperlinkParams,
	}
}

func writeSnapshot(out *strings.Builder, snapshot vaxis.FrameSnapshot) {
	cols, rows := snapshot.Dimensions()
	fmt.Fprintf(out, "size %dx%d\ncursor %+v\nplacements %+v\n", cols, rows, snapshot.Cursor(), snapshot.Placements())
	for row := 0; row < rows; row++ {
		fmt.Fprintf(out, "row %02d\n", row)
		for start := 0; start < cols; {
			first, _ := snapshot.Cell(start, row)
			kind := cellKind(first)
			graphemes := []string{first.Grapheme}
			end := start + 1
			for end < cols {
				cell, _ := snapshot.Cell(end, row)
				if cellKind(cell) != kind {
					break
				}
				graphemes = append(graphemes, cell.Grapheme)
				end++
			}
			encoded, err := json.Marshal(graphemes)
			if err != nil {
				panic(err)
			}
			fmt.Fprintf(out, "  %03d-%03d w=%d size=%d fg=%#x bg=%#x ul=%#x us=%d attr=%d link=%q params=%q %s\n",
				start, end-1, kind.Width, kind.Size, kind.Foreground, kind.Background,
				kind.UnderlineColor, kind.UnderlineStyle, kind.Attribute,
				kind.Hyperlink, kind.HyperlinkParams, encoded)
			start = end
		}
	}
}

func assertGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set UPDATE_GOLDEN=1 to create it)", err)
	}
	if string(want) != got {
		line, have, wantLine := firstDifferentLine(got, string(want))
		t.Fatalf("golden differs at line %d\nhave: %s\nwant: %s\nset UPDATE_GOLDEN=1 to update", line, have, wantLine)
	}
}

func firstDifferentLine(have, want string) (int, string, string) {
	haveLines, wantLines := strings.Split(have, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(haveLines) || i < len(wantLines); i++ {
		var h, w string
		if i < len(haveLines) {
			h = haveLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if h != w {
			return i + 1, h, w
		}
	}
	return 0, "", ""
}
