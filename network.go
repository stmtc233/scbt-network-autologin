package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"syscall"
	"time"

	"github.com/go-resty/resty/v2"
)

func GetIPv4ByInterface(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

func GetMACByInterface(name string) string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	if len(iface.HardwareAddr) == 0 {
		return ""
	}
	return strings.ToUpper(iface.HardwareAddr.String())
}

func CreateClientForInterface(ifaceName string, localIP net.IP) *resty.Client {
	jar, _ := cookiejar.New(nil)
	dialer := &net.Dialer{
		Timeout:   6 * time.Second,
		KeepAlive: 15 * time.Second,
	}

	if localIP != nil && !localIP.IsUnspecified() {
		dialer.LocalAddr = &net.TCPAddr{IP: localIP}
	}

	dialer.Control = func(network, address string, c syscall.RawConn) error {
		_ = c.Control(func(fd uintptr) {
			if ifaceName != "" {
				_ = bindToDevice(fd, ifaceName)
			}
			// 关键：打上 0x3f00 旁路标记，彻底免疫 MWAN3 策略分流与串口冲突
			_ = setSocketMark(fd, 0x3f00)
		})
		return nil
	}

	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   false,
		DisableKeepAlives:   true,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        10,
		IdleConnTimeout:     10 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}

	client := resty.NewWithClient(&http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   10 * time.Second,
	})
	client.RemoveProxy()
	return client
}

func MaskString(s string, prefixLen, suffixLen int) string {
	if len(s) <= prefixLen+suffixLen {
		return "******"
	}
	return fmt.Sprintf("%s****%s", s[:prefixLen], s[len(s)-suffixLen:])
}
