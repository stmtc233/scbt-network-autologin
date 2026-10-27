//go:build linux

package main

import "syscall"

func bindToDevice(fd uintptr, ifaceName string) error {
	if ifaceName == "" {
		return nil
	}
	return syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, ifaceName)
}
