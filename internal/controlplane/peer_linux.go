//go:build linux

package controlplane

import (
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func checkPeerUID(conn *net.UnixConn) error {
	var uid uint32
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	if err := raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil {
			err = e
			return
		}
		uid = cred.Uid
	}); err != nil {
		return err
	}
	if uid != uint32(os.Getuid()) {
		return errf("peer uid does not match daemon uid")
	}
	return nil
}
