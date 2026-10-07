package turbojpeg

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func mapFile(f *os.File, size int) ([]byte, func(), error) {
	m, err := windows.CreateFileMapping(windows.Handle(f.Fd()), nil, windows.PAGE_READONLY, 0, 0, nil)
	if err != nil {
		return nil, nil, err
	}
	addr, err := windows.MapViewOfFile(m, windows.FILE_MAP_READ, 0, 0, uintptr(size))
	if err != nil {
		windows.CloseHandle(m)
		return nil, nil, err
	}
	// the view is not go's memory, so the uintptr is all it takes
	b := unsafe.Slice((*byte)(*(*unsafe.Pointer)(unsafe.Pointer(&addr))), size)
	return b, func() {
		windows.UnmapViewOfFile(addr)
		windows.CloseHandle(m)
	}, nil
}
