//go:build !linux

package conn

import (
	"context"

	"github.com/rs/zerolog"
)

type alwaysUp struct{}

func (alwaysUp) Up() bool                 { return true }
func (alwaysUp) Changes() <-chan struct{} { return nil }

func WatchNetwork(context.Context, zerolog.Logger) Network { return alwaysUp{} }
func WatchSleep(context.Context, zerolog.Logger, func())   {}
