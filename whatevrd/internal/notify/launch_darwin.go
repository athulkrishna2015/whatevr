package notify

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>
char *launch_notification_app(const char *path);
char *bundle_identifier(const char *path);
*/
import "C"
import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

const appName = "Whatevr.app"

// helperPath finds the app this daemon belongs to: the bundle it sits in when
// run through the bin symlink, or Whatevr.app next to it in a build dir.
func helperPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(executable)
	candidates := []string{filepath.Join(dir, appName)}
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" && strings.HasSuffix(filepath.Dir(filepath.Dir(dir)), ".app") {
		candidates = []string{filepath.Dir(filepath.Dir(dir))}
	}
	for _, p := range candidates {
		if info, err := os.Stat(filepath.Join(p, "Contents", "MacOS", "Whatevr")); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("Whatevr.app is missing; run just build or install the complete macOS build")
}

// helperID is the bundle id of the app, which names its socket.
func helperID(app string) (string, error) {
	p := C.CString(app)
	defer C.free(unsafe.Pointer(p))
	id := C.bundle_identifier(p)
	if id == nil {
		return "", errors.New(app + " has no bundle identifier")
	}
	defer C.free(unsafe.Pointer(id))
	return C.GoString(id), nil
}

func launchHelper(app string) error {
	p := C.CString(app)
	defer C.free(unsafe.Pointer(p))
	failure := C.launch_notification_app(p)
	if failure == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(failure))
	return errors.New(C.GoString(failure))
}
