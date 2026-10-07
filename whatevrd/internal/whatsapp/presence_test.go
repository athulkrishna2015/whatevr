package whatsapp

import (
	"testing"
	"time"
)

func TestOnlyTimesPastTheirUseAreSwept(t *testing.T) {
	now := time.Now()
	m := map[string]time.Time{"old": now.Add(-time.Hour), "edge": now.Add(-time.Minute), "new": now}
	expire(m, now, time.Minute)
	if _, ok := m["old"]; ok || len(m) != 2 {
		t.Fatalf("kept %v", m)
	}
}
