package wa

import (
	"testing"
	"time"
)

// A message whose sender's clock is wrong pins itself to the top of the chat
// forever: nothing that arrives afterwards can sort above a timestamp in the
// future. Ordinary clock skew has to survive untouched.
func TestClampFutureTimestamp(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{"past is untouched", now.Add(-72 * time.Hour), now.Add(-72 * time.Hour)},
		{"ordinary skew is untouched", now.Add(30 * time.Second), now.Add(30 * time.Second)},
		{"just inside the slack is untouched", now.Add(futureTimestampSlack), now.Add(futureTimestampSlack)},
		{"a broken clock is pulled back", now.Add(400 * 24 * time.Hour), now},
		{"zero stays zero", time.Time{}, time.Time{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampFutureTimestamp(tc.in, now); !got.Equal(tc.want) {
				t.Fatalf("clamp = %v, want %v", got, tc.want)
			}
		})
	}
}
