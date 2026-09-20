package layout

// Text scaling is a layout input, not a decoration.
//
// A terminal discards a multicell character that does not fit on the screen,
// so a scale-3 emoji two rows from the bottom does not shrink, it vanishes.
// Every call site therefore goes through Clamp, and there is exactly one of
// them so that a second one cannot appear later and get it wrong.

// MaxScale is the largest scale the OSC 66 protocol allows.
const MaxScale = 7

// Clamp reduces a desired scale to what actually fits in a box of the given
// cell size, for a grapheme that measures width cells at scale 1.
//
// Returns 1 when nothing larger fits, which is always drawable.
func Clamp(desired, width, availCols, availRows int) int {
	if desired < 1 {
		return 1
	}
	if desired > MaxScale {
		desired = MaxScale
	}
	if width < 1 {
		width = 1
	}
	for s := desired; s > 1; s-- {
		if s*width <= availCols && s <= availRows {
			return s
		}
	}
	return 1
}

// Fits reports whether a scale would fit, for a caller choosing between
// layouts rather than shrinking one.
func Fits(scale, width, availCols, availRows int) bool {
	return scale*width <= availCols && scale <= availRows
}
