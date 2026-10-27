//go:build !linux

package main

func bindToDevice(fd uintptr, ifaceName string) error {
	return nil
}
