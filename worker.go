package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

func logWorker(name string, format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	log.Printf("[%s] %s", name, msg)
}

func RunPortWorker(ctx context.Context, ifaceCfg InterfaceConfig, pool *AccountPool, coord *Coordinator, cfg *Config, once bool) {
	label := ifaceCfg.Name
	if label == "" {
		label = ifaceCfg.Interface
	}

	isPrimary := ifaceCfg.IsPrimary || (cfg.PrimaryInterface == ifaceCfg.Interface)
	coord.RegisterPort(ifaceCfg.Interface, label, isPrimary)

	// 1. 分配初始账号
	var currentAcc *Account
	var err error
	if isPrimary {
		currentAcc, err = pool.AcquirePrimaryAccount(ifaceCfg.Interface)
	} else {
		currentAcc, err = pool.AcquireNextUnusedAccount(ifaceCfg.Interface)
	}

	if err != nil {
		logWorker(label, "初始分配账号警告: %v，稍后尝试空闲账号...", err)
	} else {
		coord.SetPortAccount(ifaceCfg.Interface, currentAcc)
		primaryTag := ""
		if isPrimary {
			primaryTag = "(主账号)"
		}
		logWorker(label, "已分配账号: %s %s (网卡: %s)", MaskString(currentAcc.Username, 3, 2), primaryTag, ifaceCfg.Interface)
	}

	var lastIP string
	portFailCount := 0
	accFailCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// 2. 检查网卡IP与MAC
		currentIP := ifaceCfg.IP
		if currentIP == "" {
			currentIP = GetIPv4ByInterface(ifaceCfg.Interface)
		}

		currentMAC := ifaceCfg.MAC
		if currentMAC == "" {
			currentMAC = GetMACByInterface(ifaceCfg.Interface)
		}

		if currentIP == "" {
			logWorker(label, "未检测到有效IPv4地址，等待DHCP分配...")
			coord.UpdatePortStatus(ifaceCfg.Interface, false, "", "")
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

		if currentIP != lastIP {
			logWorker(label, "当前IP: %s | MAC: %s", currentIP, currentMAC)
			lastIP = currentIP
			_ = EnsureInterfaceRouting(ifaceCfg.Interface, currentIP, isPrimary)
		} else {
			_ = EnsureInterfaceRouting(ifaceCfg.Interface, currentIP, isPrimary)
		}

		// 3. 创建绑定该网卡接口的独立 HTTP Client
		client := CreateClientForInterface(ifaceCfg.Interface, net.ParseIP(currentIP))

		// 4. 存活与连通性检测
		online := IsOnline(client, cfg.TestURL)
		coord.UpdatePortStatus(ifaceCfg.Interface, online, currentIP, currentMAC)

		if online {
			if portFailCount > 0 {
				logWorker(label, "接口网络恢复正常在线状态！")
				portFailCount = 0
				accFailCount = 0
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

		// 5. 当前接口离线，首先确保分配可用账号
		currentUsername := "未分配"
		if currentAcc == nil {
			if isPrimary {
				currentAcc, _ = pool.AcquirePrimaryAccount(ifaceCfg.Interface)
			} else {
				// 先检查账号池是否有空闲账号，没有就静默等待，绝不盲目轮换 MAC 导致接口反复震荡
				nextAcc, err := pool.AcquireNextUnusedAccount(ifaceCfg.Interface)
				if err != nil {
					if once {
						return
					}
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Duration(cfg.CheckInterval) * time.Second):
						continue
					}
				}
				// 确实有新账号，才轮换 MAC 准备认证
				_, _ = RotateInterfaceMAC(ifaceCfg.Interface)
				currentMAC = GetMACByInterface(ifaceCfg.Interface)
				currentIP = GetIPv4ByInterface(ifaceCfg.Interface)
				client = CreateClientForInterface(ifaceCfg.Interface, net.ParseIP(currentIP))
				_ = EnsureInterfaceRouting(ifaceCfg.Interface, currentIP, isPrimary)
				currentAcc = nextAcc
			}
			if currentAcc != nil {
				coord.SetPortAccount(ifaceCfg.Interface, currentAcc)
				accFailCount = 0
			}
		}

		portFailCount++
		accFailCount++
		if currentAcc != nil {
			currentUsername = MaskString(currentAcc.Username, 3, 2)
		}

		loginSuccess := false
		var lastLoginMsg string
		if currentAcc != nil {
			logWorker(label, "接口未在线，使用账号 [%s] 尝试认证登录 (尝试第%d次)...", currentUsername, accFailCount)
			success, msg := Login(client, currentAcc, cfg, currentIP, currentMAC)
			lastLoginMsg = msg
			if success {
				logWorker(label, "账号 [%s] 认证登录成功: %s", currentUsername, msg)
				coord.UpdatePortStatus(ifaceCfg.Interface, true, currentIP, currentMAC)
				portFailCount = 0
				accFailCount = 0
				loginSuccess = true
			} else {
				logWorker(label, "账号 [%s] 认证登录未通过: %s", currentUsername, msg)
			}
		}

		if loginSuccess {
			if once {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(cfg.CheckInterval) * time.Second):
				continue
			}
		}

		// 6. 当前账号认证失败，进行轮换与多拨智能判断
		maxRetries := cfg.RetryMaxCount
		if maxRetries <= 0 || maxRetries > 3 {
			maxRetries = 2
		}

		// 明确不可用账号特征：并发受限、欠费停机、认证拒绝等，直接判定不可用，无需等待
		isAccountUnusable := strings.Contains(lastLoginMsg, "其他终端在线") ||
			strings.Contains(lastLoginMsg, "外网测试未通") ||
			strings.Contains(lastLoginMsg, "691") ||
			strings.Contains(lastLoginMsg, "停机") ||
			strings.Contains(lastLoginMsg, "欠费") ||
			strings.Contains(lastLoginMsg, "不存在") ||
			strings.Contains(lastLoginMsg, "密码错误")

		// 瞬时物理网络抖动 (仅连接拒绝/路由不可达/超时)
		isNetworkGlitch := strings.Contains(lastLoginMsg, "网络请求错误") ||
			strings.Contains(lastLoginMsg, "connect:") ||
			strings.Contains(lastLoginMsg, "timeout")

		otherTotal, otherOnline := coord.GetOtherPortsStatus(ifaceCfg.Interface)

		// 主账号不随意轮换；普通子网卡只要重试达上限或明确账号不可用，直接换账号！
		shouldSwitchAccount := !isPrimary && (isAccountUnusable || accFailCount >= maxRetries)

		// 特殊情况保护：如果是瞬时底层网络中断且其他端口也全挂了，可能是物理拔线，等待重试
		if isNetworkGlitch && otherTotal > 0 && otherOnline == 0 && accFailCount < maxRetries {
			shouldSwitchAccount = false
		}

		if shouldSwitchAccount {
			logWorker(label, "账号 [%s] 认证失败或不可用 (%s, 累计尝试%d次)，从列表废弃...",
				currentUsername, lastLoginMsg, accFailCount)
			if currentAcc != nil {
				pool.RemoveInvalidAccount(currentAcc, "认证失败废弃: "+lastLoginMsg)
			}
			currentAcc = nil

			// 检查是否还有下一个可用账号
			nextAcc, err := pool.AcquireNextUnusedAccount(ifaceCfg.Interface)
			if err != nil {
				logWorker(label, "账号池中暂无其他空闲账号，接口保持待命...")
			} else {
				// 确实有新账号可用，才执行 MAC 轮换并立即认证
				_, _ = RotateInterfaceMAC(ifaceCfg.Interface)
				currentMAC = GetMACByInterface(ifaceCfg.Interface)
				currentIP = GetIPv4ByInterface(ifaceCfg.Interface)
				client = CreateClientForInterface(ifaceCfg.Interface, net.ParseIP(currentIP))
				_ = EnsureInterfaceRouting(ifaceCfg.Interface, currentIP, isPrimary)
				currentAcc = nextAcc
				coord.SetPortAccount(ifaceCfg.Interface, currentAcc)
				accFailCount = 0
				currentUsername = MaskString(currentAcc.Username, 3, 2)
				logWorker(label, "成功切换为新账号: [%s] (新MAC: %s, 新IP: %s)，立即尝试认证...",
					currentUsername, currentMAC, currentIP)
				s, m := Login(client, currentAcc, cfg, currentIP, currentMAC)
				if s {
					logWorker(label, "新账号 [%s] 认证登录成功: %s", currentUsername, m)
					coord.UpdatePortStatus(ifaceCfg.Interface, true, currentIP, currentMAC)
					portFailCount = 0
					accFailCount = 0
				} else {
					logWorker(label, "新账号 [%s] 认证初次尝试未通过: %s", currentUsername, m)
					accFailCount = 1
					if strings.Contains(m, "其他终端在线") || strings.Contains(m, "外网测试未通") || strings.Contains(m, "691") || strings.Contains(m, "停机") || strings.Contains(m, "欠费") || strings.Contains(m, "不存在") || strings.Contains(m, "密码错误") {
						pool.RemoveInvalidAccount(currentAcc, m)
						currentAcc = nil
					}
				}
			}
		} else {
			if !isPrimary {
				logWorker(label, "账号 [%s] 暂时保持，等待下个周期重试 (%d/%d)...",
					currentUsername, accFailCount, maxRetries)
			}
		}

		if once {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(cfg.CheckInterval) * time.Second):
			continue
		}
	}
}
