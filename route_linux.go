//go:build linux

package main

import (
	"fmt"
	"log"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// EnsureInterfaceRouting 在 Linux / OpenWrt 上配置多网卡路由与 ARP 隔离
// 1. 设置 ARP 隔离 (arp_ignore=1, arp_announce=2)，防止同子网下的多网卡 MAC 冲突
// 2. 主网卡 (eth1): 确保在 main 表中的默认网关 metric 为 10，优先走主账号
// 3. 虚拟网卡 (vwan1, vwan2 等): 创建独立的策略路由表 (table 201, 202 等)，
//    使得绑定该网卡 IP 的出站连接（如 Portal 认证、多线程测速）能够正确从各自网卡发出
func EnsureInterfaceRouting(iface string, ipAddr string, isPrimary bool) error {
	if iface == "" || ipAddr == "" {
		return nil
	}

	// 1. 设置 ARP 隔离
	_ = exec.Command("sysctl", "-w", "net.ipv4.conf.all.arp_ignore=1").Run()
	_ = exec.Command("sysctl", "-w", "net.ipv4.conf.all.arp_announce=2").Run()
	_ = exec.Command("sysctl", "-w", fmt.Sprintf("net.ipv4.conf.%s.arp_ignore=1", iface)).Run()
	_ = exec.Command("sysctl", "-w", fmt.Sprintf("net.ipv4.conf.%s.arp_announce=2", iface)).Run()

	// 2. 获取该接口的默认网关
	gateway := ""
	out, err := exec.Command("ip", "route", "show", "default", "dev", iface).Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
				gateway = fields[2]
				break
			}
		}
	}
	if gateway == "" {
		// 回退：从该接口的所有路由中找网关
		out2, _ := exec.Command("ip", "route", "show", "dev", iface).Output()
		for _, line := range strings.Split(string(out2), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
				gateway = fields[2]
				break
			}
		}
	}

	// 如果未能自动获取，回退到校园网通用网关 10.218.224.1
	if gateway == "" {
		gateway = "10.218.224.1"
	}

	if isPrimary {
		// 主网卡：在 main 表中确保 metric 为 10
		_ = exec.Command("ip", "route", "replace", "default", "via", gateway, "dev", iface, "metric", "10").Run()
		log.Printf("[%s] 主接口路由优先级已确认 (metric 10, 网关 %s)", iface, gateway)
	} else {
		// 虚拟网卡：分配独立策略路由表 (例如 200 + 编号)
		tableID := 200
		numPart := strings.TrimPrefix(iface, "vwan")
		if n, err := strconv.Atoi(numPart); err == nil {
			tableID = 200 + n
		} else {
			sum := 0
			for _, c := range iface {
				sum += int(c)
			}
			tableID = 100 + (sum % 80)
		}
		if tableID >= 250 {
			tableID = 200 + (tableID % 40)
		}
		tableStr := strconv.Itoa(tableID)

		// 检查策略路由规则是否已存在
		ruleCheck, _ := exec.Command("ip", "rule", "show").Output()
		fromPattern := fmt.Sprintf("from %s lookup %s", ipAddr, tableStr)
		if !strings.Contains(string(ruleCheck), fromPattern) {
			_ = exec.Command("ip", "rule", "add", "from", ipAddr, "lookup", tableStr).Run()
		}

		// 获取直连子网网段
		subnet := "10.218.224.0/20"
		if outSubnet, errSub := exec.Command("ip", "route", "show", "dev", iface, "proto", "kernel").Output(); errSub == nil {
			fields := strings.Fields(string(outSubnet))
			if len(fields) > 0 && strings.Contains(fields[0], "/") {
				subnet = fields[0]
			}
		}

		// 在独立策略路由表中设置直连子网与默认网关，确保ARP与下一跳闭环
		_ = exec.Command("ip", "route", "replace", subnet, "dev", iface, "proto", "kernel", "scope", "link", "src", ipAddr, "table", tableStr).Run()
		_ = exec.Command("ip", "route", "replace", "default", "via", gateway, "dev", iface, "table", tableStr).Run()
		log.Printf("[%s] 虚拟接口策略路由已配置 (table %s, from %s, via %s)", iface, tableStr, ipAddr, gateway)
	}

	_ = exec.Command("ip", "route", "flush", "cache").Run()
	return nil
}

// 兼容保留旧函数名称
func EnsureInterfaceMetric(iface string, metric int) error {
	return nil
}

// RotateInterfaceMAC 在切换账号前，为虚拟网卡或副网卡分配全新的随机 MAC 地址并触发 OpenWrt netifd 重联
func RotateInterfaceMAC(iface string) (string, error) {
	if iface == "" || iface == "wan" || iface == "lan1" || iface == "wwan" || iface == "apclix0" {
		return "", nil
	}

	r1 := rand.Intn(256)
	r2 := rand.Intn(256)
	newMAC := fmt.Sprintf("82:af:ca:b5:%02x:%02x", r1, r2)

	var ifupTarget string
	if strings.HasPrefix(iface, "vwan") {
		uciSection := fmt.Sprintf("network.dev_%s.macaddr", iface)
		_ = exec.Command("uci", "set", fmt.Sprintf("%s=%s", uciSection, newMAC)).Run()
		_ = exec.Command("uci", "commit", "network").Run()
		ifupTarget = iface
	} else if iface == "lan2" || iface == "wan2" {
		_ = exec.Command("uci", "set", fmt.Sprintf("network.dev_lan2.macaddr=%s", newMAC)).Run()
		_ = exec.Command("uci", "set", fmt.Sprintf("network.wan2.macaddr=%s", newMAC)).Run()
		_ = exec.Command("uci", "commit", "network").Run()
		_ = exec.Command("ip", "link", "set", "dev", "lan2", "address", newMAC).Run()
		ifupTarget = "wan2"
	} else {
		return "", nil
	}

	_ = exec.Command("ifup", ifupTarget).Run()

	log.Printf("[%s] 触发网卡MAC轮换 (新MAC: %s)，等待网络重联获取新IP...", iface, newMAC)

	// 等待 netifd 完成 DHCP 握手，最多等待 15 秒
	for i := 0; i < 15; i++ {
		time.Sleep(1 * time.Second)
		ip := GetIPv4ByInterface(iface)
		mac := GetMACByInterface(iface)
		if ip != "" && mac != "" && strings.EqualFold(mac, newMAC) {
			log.Printf("[%s] MAC轮换完成，成功获取新IP: %s (MAC: %s)", iface, ip, mac)
			return mac, nil
		}
	}
	return newMAC, nil
}
