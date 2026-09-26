package layout

import "testing"

func TestClampReducesRatherThanOverflowing(t *testing.T) {
	tests := []struct {
		name                             string
		desired, width, cols, rows, want int
	}{
		{"fits outright", 3, 1, 20, 10, 3},
		{"short of rows", 3, 1, 20, 2, 2},
		{"one row left", 3, 1, 20, 1, 1},
		{"short of columns", 3, 2, 4, 10, 2},
		{"nothing fits", 3, 2, 3, 1, 1},
		{"already one", 1, 1, 80, 24, 1},
		{"over the protocol maximum", 99, 1, 200, 200, MaxScale},
		{"nonsense desired", 0, 1, 80, 24, 1},
		{"nonsense width", 2, 0, 80, 24, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Clamp(tc.desired, tc.width, tc.cols, tc.rows)
			if got != tc.want {
				t.Errorf("Clamp(%d,%d,%d,%d) = %d, want %d",
					tc.desired, tc.width, tc.cols, tc.rows, got, tc.want)
			}
		})
	}
}

func TestClampNeverReturnsSomethingThatWouldBeDiscarded(t *testing.T) {
	// The property that matters: whatever comes back has to fit, because a
	// block that does not fit is not drawn small, it is not drawn at all.
	for cols := 1; cols <= 12; cols++ {
		for rows := 1; rows <= 8; rows++ {
			for width := 1; width <= 3; width++ {
				got := Clamp(MaxScale, width, cols, rows)
				if got != 1 && !Fits(got, width, cols, rows) {
					t.Fatalf("Clamp gave %d for %dx%d at width %d, which does not fit",
						got, cols, rows, width)
				}
			}
		}
	}
}
