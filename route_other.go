//go:build !linux

package main

func EnsureInterfaceMetric(iface string, metric int) error {
	return nil
}
