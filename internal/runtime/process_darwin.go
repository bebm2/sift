//go:build darwin

package runtime

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	procInfoCallPIDInfo = 0x2
	procPIDPathInfo     = 11
	procPIDPathInfoMax  = 4 * 1024
)

func darwinKinfo(pid int) (*unix.KinfoProc, error) {
	return unix.SysctlKinfoProc("kern.proc.pid", pid)
}

func darwinStartMS(kp *unix.KinfoProc) int64 {
	return kp.Proc.P_starttime.Sec*1000 + int64(kp.Proc.P_starttime.Usec)/1000
}

func darwinPIDPath(pid int) (string, error) {
	buf := make([]byte, procPIDPathInfoMax)
	_, _, errno := unix.RawSyscall6(
		unix.SYS_PROC_INFO,
		procInfoCallPIDInfo,
		uintptr(pid),
		procPIDPathInfo,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if errno != 0 {
		return "", errno
	}
	path := unix.ByteSliceToString(buf)
	if path == "" {
		return "", errors.New("runtime: empty darwin pid path")
	}
	return path, nil
}

func darwinProcessAbsent(pid int) bool {
	_, err := unix.Getpgid(pid)
	return errors.Is(err, unix.ESRCH)
}
