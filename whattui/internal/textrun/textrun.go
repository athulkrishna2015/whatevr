// Package textrun draws the scripts a terminal cell grid cannot hold.
//
// Devanagari and its neighbours reorder, stack and join, and their spacing
// marks carry width that every terminal width table scores as zero. The
// terminal therefore allots a fraction of the columns the text needs and
// overlaps the rest, and no amount of width pinning fixes that: the cell
// count is the contract, and the text does not fit it.
//
// So for those scripts, and only those, we shape and rasterise the word
// ourselves at the terminal's own em and baseline, and place the result as a
// graphic over blank cells. The cells keep their background and their
// underline; only the ink comes from us.
//
// Lifted from pawbar's internal/textrun, which is where this pipeline was
// built and proven. Kept close to that copy on purpose: the two should stay
// diffable.
package textrun

import (
	"sync"
)

// Options configure the shaper. Everything here is known at startup.
type Options struct {
	// Kitty is the kitty binary to ask for font metrics, empty to skip the
	// probe. whattui runs inside the terminal it is drawing for, so this is
	// set only when that terminal is a kitty.
	Kitty string
	// Family overrides the font family, empty to take the terminal's own.
	Family string
	// FontSize in points, zero to take whatever the config says.
	FontSize float64
	// BaselineNudge shifts every rasterised run vertically, for the case
	// where the probe is unavailable and the derived baseline is a pixel
	// out.
	BaselineNudge int
	// Notify is called once when the shaper becomes usable, so the caller
	// can repaint the frames that fell back to plain cells.
	Notify func()
}

// Shaper rasterises the complex scripts one surface draws. It is per surface,
// not per process: everything it measures comes off the terminal's cell, so
// two surfaces at different sizes would each throw the other's shaper away on
// every cell-size change.
type Shaper struct {
	mu   sync.RWMutex
	opts Options
	// cellW, cellH is the terminal's cell in pixels, as vaxis measured it.
	cellW, cellH int

	// gen counts how many times the shaper has been thrown away, so a build
	// that was already running when that happened knows not to install
	// itself. loadOnce starts the next one, and is replaced with it.
	gen      int
	loadOnce *sync.Once
	loaded   bool
	state    *fontset
}

// New records how to build the shaper. It does no work: the shaper only comes
// up if a complex run actually turns up, and the font index alone costs more
// than the rest of the frontend put together.
func New(o Options) *Shaper {
	return &Shaper{opts: o, loadOnce: &sync.Once{}}
}

// SetCellSize records the terminal's cell in pixels. Everything the shaper
// measures comes off the cell, so a change throws it away and rebuilds it.
func (sh *Shaper) SetCellSize(w, h int) {
	if sh == nil {
		return
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if w == sh.cellW && h == sh.cellH {
		return
	}
	sh.cellW, sh.cellH = w, h
	sh.invalidate()
}

// invalidate throws the shaper away so the next complex run builds a fresh
// one. Callers hold sh.mu.
func (sh *Shaper) invalidate() {
	sh.gen++
	sh.loaded = false
	sh.state = nil
	sh.loadOnce = &sync.Once{}
}

// ready reports whether the shaper can be used, starting it the first time it
// is asked. Until it is up, callers fall back to plain terminal cells; the
// Notify hook repaints once that stops being necessary.
func (sh *Shaper) ready() (*fontset, bool) {
	sh.mu.RLock()
	if sh.loaded {
		s := sh.state
		sh.mu.RUnlock()
		return s, s != nil
	}
	o, w, h, g, once := sh.opts, sh.cellW, sh.cellH, sh.gen, sh.loadOnce
	sh.mu.RUnlock()

	if w <= 0 || h <= 0 {
		return nil, false
	}
	once.Do(func() {
		go func() {
			s := newFontset(o, w, h)
			sh.mu.Lock()
			// Something already threw this build away while it ran.
			stale := sh.gen != g
			if !stale {
				sh.state, sh.loaded = s, true
			}
			sh.mu.Unlock()
			if !stale && s != nil && o.Notify != nil {
				o.Notify()
			}
		}()
	})
	return nil, false
}

// CellSize reports the pixel cell the shaper lays out for. A nil Shaper has no
// cell of its own.
func (sh *Shaper) CellSize() (int, int) {
	if sh == nil {
		return 0, 0
	}
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	return sh.cellW, sh.cellH
}

// Begin opens a frame and reports whether runs can be shaped during it,
// starting the shaper the first time it is asked.
//
// Snapshotting once per frame rather than asking per string is what keeps
// measuring and drawing from disagreeing across the moment the shaper comes
// up: a layout measured in terminal cells and drawn as a graphic is a layout
// with a hole in it.
func (sh *Shaper) Begin() bool {
	if sh == nil {
		return false
	}
	_, ok := sh.ready()
	return ok
}
