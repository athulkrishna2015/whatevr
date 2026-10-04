package notify

/*
#cgo CFLAGS: -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>
char *launch_notification_app(const char *path);
*/
import "C"
import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"
)

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
	for _, p := range []string{filepath.Join(dir, "Whatevr Notifications.app"), filepath.Join(filepath.Dir(dir), "libexec", "Whatevr Notifications.app")} {
		if info, err := os.Stat(filepath.Join(p, "Contents", "MacOS", "WhatevrNotifications")); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("Whatevr Notifications.app is missing; run just build or install the complete macOS build")
}
func launchHelper() error {
	path, err := helperPath()
	if err != nil {
		return err
	}
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	failure := C.launch_notification_app(p)
	if failure == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(failure))
	return errors.New(C.GoString(failure))
}
