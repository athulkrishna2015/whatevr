package server

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// samePeer refuses a peer running as another user.
func samePeer(nc net.Conn) error {
	u, ok := nc.(*net.UnixConn)
	if !ok {
		return nil
	}
	raw, err := u.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) { cred, cerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if cred.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("peer uid %d", cred.Uid)
	}
	return nil
}
