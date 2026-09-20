package ui

import (
	"image"

	"go.rockorager.dev/vaxis"

	"whattui/internal/layout"
	"whattui/internal/paint"
	"whattui/internal/term"
)

// A surface is chrome we rasterised ourselves, waiting for the end of the
// frame. Held rather than drawn on the spot for the same reason a run is: the
// panes paint over each other freely, and an image placed mid-frame would sit
// under whatever the next pane blanked.
//
// Placed under the terminal's own text and over the cell background, which is
// the whole technique: the shapes are ours and every glyph on top of them is
// still the user's font, still selectable, still a real cell.
const chromeZ = -1

type surface struct {
	// win is the cells the visible part lands on, and src is the part of the
	// spec's own pixels those cells show. A surface clipped by a pane edge or
	// covered by a panel is the same upload placed at a different source
	// rectangle, never a second image of what is left.
	win  vaxis.Window
	spec paint.Spec
	src  image.Rectangle
	// Absolute cells, so occlusion can compare against a panel's rect.
	col, row, w, h int
}

// painted says whether chrome is drawn rather than typed. Below the graphics
// tiers, and anywhere the terminal will not say how big a cell is, the box
// glyphs draw the same geometry.
func (a *App) painted() bool {
	if a.caps.Tier < term.TierGraphics {
		return false
	}
	w, h := a.cellPix()
	return w > 0 && h > 0
}

// cellPix is the terminal's cell in pixels. Asked of the terminal rather than
// of the shaper, because chrome is drawn whether or not a font ever loaded.
func (a *App) cellPix() (int, int) {
	size := a.vx.Size()
	if size.Cols <= 0 || size.Rows <= 0 {
		return 0, 0
	}
	return size.XPixel / size.Cols, size.YPixel / size.Rows
}

// paintRect reserves a rectangle of cells for a surface. build is handed the
// pixel size of the whole rectangle, including the part of it the window
// clips away: what is clipped is a smaller placement of the same image, not a
// smaller image.
func (a *App) paintRect(win vaxis.Window, col, row, w, h int, build func(pw, ph int) paint.Spec) {
	cw, ch := a.cellPix()
	if cw <= 0 || ch <= 0 || w <= 0 || h <= 0 {
		return
	}
	winW, winH := win.Size()
	left, top := maxInt(col, 0), maxInt(row, 0)
	right, bottom := minInt(col+w, winW), minInt(row+h, winH)
	if right <= left || bottom <= top {
		return
	}
	oc, or := win.Origin()
	a.surfaces = append(a.surfaces, surface{
		win:  win.New(left, top, right-left, bottom-top),
		spec: build(w*cw, h*ch),
		src: image.Rect(
			(left-col)*cw, (top-row)*ch,
			(right-col)*cw, (bottom-row)*ch,
		),
		col: oc + left, row: or + top, w: right - left, h: bottom - top,
	})
}

// cut narrows a surface to a rectangle of cells inside the one it already
// holds. The source rectangle follows, so the pixels stay where they were.
func (s surface) cut(col, row, w, h, cellW, cellH int) surface {
	dx, dy := col-s.col, row-s.row
	q := s
	q.win = s.win.New(dx, dy, w, h)
	q.src = image.Rect(
		s.src.Min.X+dx*cellW, s.src.Min.Y+dy*cellH,
		s.src.Min.X+(dx+w)*cellW, s.src.Min.Y+(dy+h)*cellH,
	)
	q.col, q.row, q.w, q.h = col, row, w, h
	return q
}

// occludeSurfaces drops the chrome a later pane covered. An image at a
// negative z is drawn over the cell background, so a panel filled on top of a
// bubble does not hide it: the placement itself has to go.
func (a *App) occludeSurfaces(r layout.Rect) {
	cw, ch := a.cellPix()
	if cw <= 0 || ch <= 0 {
		return
	}
	kept := a.occludedSurfaces[:0]
	for _, s := range a.surfaces {
		if s.row >= r.Row+r.Height || s.row+s.h <= r.Row ||
			s.col >= r.Col+r.Width || s.col+s.w <= r.Col {
			kept = append(kept, s)
			continue
		}
		// What is left of a rectangle with a rectangle taken out of it: the
		// rows above, the rows below, and the two ends of the band between.
		if s.row < r.Row {
			kept = append(kept, s.cut(s.col, s.row, s.w, r.Row-s.row, cw, ch))
		}
		if end := r.Row + r.Height; s.row+s.h > end {
			kept = append(kept, s.cut(s.col, end, s.w, s.row+s.h-end, cw, ch))
		}
		top, bottom := maxInt(s.row, r.Row), minInt(s.row+s.h, r.Row+r.Height)
		if s.col < r.Col {
			kept = append(kept, s.cut(s.col, top, r.Col-s.col, bottom-top, cw, ch))
		}
		if end := r.Col + r.Width; s.col+s.w > end {
			kept = append(kept, s.cut(end, top, s.col+s.w-end, bottom-top, cw, ch))
		}
	}
	a.occludedSurfaces = a.surfaces[:0]
	a.surfaces = kept
}

// flushSurfaces uploads what this frame drew that is new and places all of it.
func (a *App) flushSurfaces() {
	cw, ch := a.cellPix()
	if cw <= 0 || ch <= 0 {
		return
	}
	for _, s := range a.surfaces {
		k := imgKey{key: s.spec.Key(), w: cw, h: ch}
		a.seen[k] = true
		img, ok := a.images[k]
		if !ok {
			img = a.vx.NewKittyPixels(s.spec.Render())
			a.images[k] = img
		}
		pw, ph := s.spec.Size()
		if s.src.Min.X == 0 && s.src.Min.Y == 0 && s.src.Dx() == pw && s.src.Dy() == ph {
			img.SetSourceRect(0, 0, 0, 0)
		} else {
			img.SetSourceRect(s.src.Min.X, s.src.Min.Y, s.src.Dx(), s.src.Dy())
		}
		img.SetZIndex(chromeZ)
		img.Draw(s.win)
	}
}
