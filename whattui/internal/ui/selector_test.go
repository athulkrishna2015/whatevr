package ui

import "testing"

func TestSelectorKeyboardMouseAndClipping(t *testing.T) {
	var s selector[string]
	s.Set([]string{"zero", "one", "two", "three", "four"})

	s.Move(3, 2)
	if got, _ := s.Current(); got != "three" || s.top != 2 {
		t.Fatalf("move selected %q at top %d, want three at top 2", got, s.top)
	}
	if got := s.Visible(2); len(got) != 2 || got[0] != "two" || got[1] != "three" {
		t.Fatalf("visible = %#v, want [two three]", got)
	}
	if !s.Hover(0, 2) || s.hovered != 2 {
		t.Fatalf("hovered = %d, want 2", s.hovered)
	}
	if got, ok := s.Click(1, 2); !ok || got != "three" {
		t.Fatalf("click = %q, %v, want three", got, ok)
	}
	s.Wheel(10, 2)
	if s.top != 3 {
		t.Fatalf("wheel top = %d, want clipped top 3", s.top)
	}
	s.Wheel(-10, 2)
	if s.top != 0 {
		t.Fatalf("reverse wheel top = %d, want 0", s.top)
	}
	s.Hover(9, 2)
	if s.hovered != -1 {
		t.Fatalf("out-of-range hover = %d, want -1", s.hovered)
	}
}

func TestSelectorHandlesTinyViewport(t *testing.T) {
	var s selector[int]
	s.Set([]int{1, 2})
	s.Move(1, 0)
	s.Wheel(1, 0)
	if got := s.Visible(0); got != nil {
		t.Fatalf("visible in zero rows = %#v, want nil", got)
	}
}
