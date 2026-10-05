package frontends

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>
char *frontend_apps(void);
char *open_frontend_app(const char *path);
*/
import "C"
import (
	"errors"
	"strings"
	"unsafe"
)

// native is every app that declares the whatevr-frontend scheme and names
// itself with WhatevrFrontendID, wherever launch services found it.
func native() []Frontend {
	raw := C.frontend_apps()
	if raw == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(raw))
	var out []Frontend
	for _, line := range strings.Split(C.GoString(raw), "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 || !ValidID(parts[1]) {
			continue
		}
		out = append(out, Frontend{ID: parts[1], Name: parts[2], App: parts[0], Source: Native, From: parts[0]})
	}
	return out
}

func openApp(path string) error {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	failure := C.open_frontend_app(p)
	if failure == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(failure))
	return errors.New(C.GoString(failure))
}
