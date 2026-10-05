//go:build !darwin

package frontends

func native() []Frontend { return desktopFrontends(desktopDirs()) }
