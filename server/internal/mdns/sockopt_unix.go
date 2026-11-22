//go:build linux || darwin

package mdns

import "syscall"

func setReuseAddrPort(fd uintptr) error {
	if err := syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		return err
	}
	// SO_REUSEPORT is 15 on Linux and 0x0200 on Darwin; best-effort via platform constant if defined.
	_ = setReusePort(int(fd))
	return nil
}
