//go:build !linux

package main

func EnsureInterfaceRouting(iface string, ipAddr string, isPrimary bool) error {
	return nil
}

func EnsureInterfaceMetric(iface string, metric int) error {
	return nil
}

func RotateInterfaceMAC(iface string) (string, error) {
	return "", nil
}
