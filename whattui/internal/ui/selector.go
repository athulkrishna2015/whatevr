package ui

// selector is reusable selection state. It deliberately knows nothing about
// terminals, commands, or drawing; callers translate their own coordinates
// into item rows.
type selector[T any] struct {
	items    []T
	selected int
	top      int
	hovered  int
}

func (s *selector[T]) Set(items []T) {
	s.items = append(s.items[:0], items...)
	s.selected = 0
	s.top = 0
	s.hovered = -1
}

func (s *selector[T]) Items() []T { return s.items }

func (s *selector[T]) Current() (T, bool) {
	var zero T
	if s.selected < 0 || s.selected >= len(s.items) {
		return zero, false
	}
	return s.items[s.selected], true
}

func (s *selector[T]) Move(by, visible int) {
	if len(s.items) == 0 {
		return
	}
	s.selected = clamp(s.selected+by, 0, len(s.items)-1)
	s.reveal(visible)
}

func (s *selector[T]) Hover(row, visible int) bool {
	at := s.top + row
	if row < 0 || row >= visible || at >= len(s.items) {
		at = -1
	}
	changed := at != s.hovered
	s.hovered = at
	return changed
}

func (s *selector[T]) Click(row, visible int) (T, bool) {
	s.Hover(row, visible)
	if s.hovered < 0 {
		var zero T
		return zero, false
	}
	s.selected = s.hovered
	return s.items[s.selected], true
}

func (s *selector[T]) Wheel(by, visible int) {
	if visible <= 0 {
		return
	}
	maxTop := len(s.items) - visible
	if maxTop < 0 {
		maxTop = 0
	}
	s.top = clamp(s.top+by, 0, maxTop)
	if s.selected < s.top {
		s.selected = s.top
	}
	if visible > 0 && s.selected >= s.top+visible {
		s.selected = s.top + visible - 1
	}
}

func (s *selector[T]) Visible(visible int) []T {
	if visible <= 0 || s.top >= len(s.items) {
		return nil
	}
	end := minInt(s.top+visible, len(s.items))
	return s.items[s.top:end]
}

func (s *selector[T]) reveal(visible int) {
	if visible <= 0 {
		return
	}
	if s.selected < s.top {
		s.top = s.selected
	}
	if s.selected >= s.top+visible {
		s.top = s.selected - visible + 1
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
