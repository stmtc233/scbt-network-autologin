//go:build linux

package main

import "syscall"

const (
	SO_MARK = 36 // Linux SOL_SOCKET SO_MARK
)

func bindToDevice(fd uintptr, ifaceName string) error {
	if ifaceName == "" {
		return nil
	}
	return syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifaceName)
}

func setSocketMark(fd uintptr, mark int) error {
	if mark <= 0 {
		return nil
	}
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, SO_MARK, mark)
}
