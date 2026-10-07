//go:build !darwin

package notify

import (
	"context"
	"errors"
)

func Configure(context.Context, bool) (string, error) {
	return "", errors.New("notification setup commands are macOS-only; Linux uses the desktop notification service")
}
