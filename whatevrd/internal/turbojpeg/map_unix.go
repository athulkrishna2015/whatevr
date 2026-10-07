//go:build unix

package turbojpeg

import (
	"os"

	"golang.org/x/sys/unix"
)

func mapFile(f *os.File, size int) ([]byte, func(), error) {
	b, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	return b, func() { unix.Munmap(b) }, nil
}
