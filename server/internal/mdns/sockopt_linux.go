//go:build linux

package mdns

import "syscall"

func setReusePort(fd int) error {
	const soReusePort = 15
	return syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, soReusePort, 1)
}
