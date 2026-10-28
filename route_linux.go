//go:build linux

package main

import (
	"log"
	"os/exec"
	"strings"
)

// 在 Linux / OpenWrt 上确保主网卡接口路由跃点数(metric)为最低优先级数值 (例如 10)
// 使得系统默认流量、局域网单连接流量优先经由主网卡发出
func EnsureInterfaceMetric(iface string, metric int) error {
	if iface == "" {
		return nil
	}

	// 查找当前接口的默认网关
	out, err := exec.Command("ip", "route", "show", "default", "dev", iface).Output()
	if err != nil {
		return err
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "default" && fields[1] == "via" {
			gateway := fields[2]
			// 检查是否已经具有该 metric
			hasMetric := false
			for i, f := range fields {
				if f == "metric" && i+1 < len(fields) && fields[i+1] == "10" {
					hasMetric = true
					break
				}
			}
			if !hasMetric {
				// 替换为指定 metric
				cmd := exec.Command("ip", "route", "replace", "default", "via", gateway, "dev", iface, "metric", "10")
				if err := cmd.Run(); err != nil {
					log.Printf("[%s] 调整主接口路由跃点失败: %v", iface, err)
					return err
				}
				log.Printf("[%s] 已设置主接口路由优先级 (metric: %d, 默认流量优先通过)", iface, metric)
			}
		}
	}
	return nil
}
