package app

/*
#include <launch.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"
)

// launchdSocket is the Sockets key the LaunchAgent declares.
const launchdSocket = "Listeners"

// LaunchdListener returns the listening socket launchd holds for the
// LaunchAgent, or (nil, nil) when launchd didn't start this process with one.
func LaunchdListener() (net.Listener, error) {
	name := C.CString(launchdSocket)
	defer C.free(unsafe.Pointer(name))
	var fds *C.int
	var n C.size_t
	if rc := C.launch_activate_socket(name, &fds, &n); rc != 0 {
		// ESRCH: not a launchd job, ENOENT: a job without this socket
		if e := syscall.Errno(rc); e == syscall.ESRCH || e == syscall.ENOENT {
			return nil, nil
		}
		return nil, fmt.Errorf("launch_activate_socket: %w", syscall.Errno(rc))
	}
	defer C.free(unsafe.Pointer(fds))
	all := unsafe.Slice(fds, int(n))
	if len(all) != 1 {
		for _, fd := range all {
			_ = syscall.Close(int(fd))
		}
		return nil, fmt.Errorf("expected exactly one socket from launchd, got %d", len(all))
	}
	fd := int(all[0])
	syscall.CloseOnExec(fd)

	file := os.NewFile(uintptr(fd), "whatevrd-activation-socket")
	if file == nil {
		return nil, fmt.Errorf("invalid socket-activation file descriptor %d", fd)
	}
	defer file.Close()

	listener, err := net.FileListener(file)
	if err != nil {
		return nil, fmt.Errorf("adopt launchd socket: %w", err)
	}
	return listener, nil
}
