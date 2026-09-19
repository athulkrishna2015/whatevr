package wa

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"whatevrd/internal/app"
)

// An expired code is the ordinary case: somebody is looking at the screen and
// needs the next one now. A QR attempt that actually failed is not, and
// retrying those every 300ms is a hammer.
func TestQRRetryBacksOffOnFailuresButNotOnExpiry(t *testing.T) {
	expired := connectionRetry(9, fmt.Errorf("QR login timed out: %w", errQRCodeExpired))
	if expired.delay != qrRetryDelay {
		t.Fatalf("an expired code waited %s, want %s", expired.delay, qrRetryDelay)
	}
	if expired.attempt != 0 {
		t.Fatalf("an expired code counted as attempt %d, want 0", expired.attempt)
	}
	if expired.state != app.StateNeedLogin {
		t.Fatalf("an expired code left state %v", expired.state)
	}

	var last time.Duration
	for attempt := range 10 {
		plan := connectionRetry(attempt, errors.Join(errQRLoginRetry, errors.New("boom")))
		if plan.state != app.StateNeedLogin {
			t.Fatalf("a failed QR attempt left state %v", plan.state)
		}
		if plan.delay < last {
			t.Fatalf("delay went backwards: %s after %s", plan.delay, last)
		}
		if plan.delay > qrBackoffMax {
			t.Fatalf("delay %s exceeded the cap %s", plan.delay, qrBackoffMax)
		}
		last = plan.delay
	}
	if last <= qrRetryDelay {
		t.Fatalf("repeated failures never backed off past %s", qrRetryDelay)
	}
}
