package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

func logWorker(name string, format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("[%s] %s", name, msg)
}

func RunAccountWorker(ctx context.Context, acc AccountConfig, cfg *Config, once bool) {
	label := acc.Name
	if label == "" {
		label = acc.Interface
	}

	logWorker(label, "已启动接口监控 (账号: %s, 网卡: %s)", MaskString(acc.Username, 3, 2), acc.Interface)

	retryCount := 0
	var lastIP string

	for {
		// 1. 获取接口当前IP与MAC
		currentIP := acc.IP
		if currentIP == "" {
			currentIP = GetIPv4ByInterface(acc.Interface)
		}

		currentMAC := acc.MAC
		if currentMAC == "" {
			currentMAC = GetMACByInterface(acc.Interface)
		}

		if currentIP == "" {
			logWorker(label, "未检测到有效IPv4地址，等待DHCP分配...")
			if once {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		if currentMAC == "" {
			logWorker(label, "警告: 未能获取到MAC地址，可能影响认证。")
		}

		if currentIP != lastIP {
			logWorker(label, "当前IP: %s | MAC: %s", currentIP, currentMAC)
			lastIP = currentIP
		}

		// 2. 创建绑定该接口与源IP的独立 HTTP Client
		client := CreateClientForInterface(acc.Interface, net.ParseIP(currentIP))

		// 3. 检测该接口是否在线
		if IsOnline(client, cfg.TestURL) {
			if retryCount > 0 {
				logWorker(label, "网络恢复正常在线状态！")
				retryCount = 0
			}
			if once {
				logWorker(label, "接口已在线，单次检测完成。")
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(cfg.CheckInterval) * time.Second):
				continue
			}
		}

		// 4. 网络离线，执行认证
		if retryCount >= cfg.RetryMaxCount {
			logWorker(label, "已连续失败 %d 次，暂停 10 分钟后继续尝试...", cfg.RetryMaxCount)
			if once {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Minute):
				retryCount = 0
				continue
			}
		}

		logWorker(label, "检测到离线，正在进行第 %d 次认证尝试...", retryCount+1)
		success, msg := Login(client, &acc, cfg, currentIP, currentMAC)
		if success {
			logWorker(label, "认证成功: %s", msg)
			retryCount = 0
			if once {
				return
			}
		} else {
			retryCount++
			logWorker(label, "认证失败: %s", msg)
			if once {
				return
			}
			time.Sleep(3 * time.Second)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(cfg.CheckInterval) * time.Second):
		}
	}
}
