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
			}
			if isPrimary && cfg.SetTrafficPriority {
				_ = EnsureInterfaceMetric(ifaceCfg.Interface, 10)
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

		// 5. 当前接口离线，进行跨端口存活交叉分析 (核心多拨智能判断)
		portFailCount++
		otherTotal, otherOnline := coord.GetOtherPortsStatus(ifaceCfg.Interface)

		currentUsername := "未分配"
		if currentAcc != nil {
			currentUsername = MaskString(currentAcc.Username, 3, 2)
		}

		// 情况 A: 其他所有端口也全部离线 (otherTotal > 0 且 otherOnline == 0)
		// 说明是校园网整体断网（如机房维护、物理网线松脱、夜间断网），切勿乱换账号！
		if otherTotal > 0 && otherOnline == 0 {
			logWorker(label, "检测到接口离线，但其他所有接口 (%d个) 也均处于离线状态！判定为校园网整体中断，保持当前账号 [%s]，等待网络恢复...",
				otherTotal, currentUsername)
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

		// 情况 B: 仅当前端口离线，而其他至少一个端口正常在线！
		// 确凿证明校园网完全通畅，当前网卡离线 100% 是由于该账号被踢下线、欠费或并发受限！
		if otherTotal > 0 && otherOnline > 0 {
			logWorker(label, "检测到仅当前接口离线（其他 %d 个接口正常在线）！判定当前账号 [%s] 失效或被踢下线",
				otherOnline, currentUsername)

			// 先尝试使用当前账号快速重连一次 (应对偶尔的Portal会话超时)
			reconnectSuccess := false
			if currentAcc != nil && portFailCount <= 1 {
				logWorker(label, "尝试使用原账号 [%s] 重新认证一次...", currentUsername)
				s, m := Login(client, currentAcc, cfg, currentIP, currentMAC)
				if s {
					logWorker(label, "原账号重新认证成功: %s", m)
					coord.UpdatePortStatus(ifaceCfg.Interface, true, currentIP, currentMAC)
					if isPrimary && cfg.SetTrafficPriority {
						_ = EnsureInterfaceMetric(ifaceCfg.Interface, 10)
					}
					portFailCount = 0
					reconnectSuccess = true
				}
			}

			if reconnectSuccess {
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

			// 原账号重连无效：立即切换到下一个未使用的账号！
			logWorker(label, "当前账号不可用，标记为失败并切换到下一个未使用的空闲账号...")
			if currentAcc != nil {
				pool.MarkAccountFailed(currentAcc, "单端口异常掉线且重连失败")
			}

			nextAcc, err := pool.AcquireNextUnusedAccount(ifaceCfg.Interface)
			if err != nil {
				logWorker(label, "切换账号失败: %v。账号池状态: [%s]", err, pool.GetStats())
				if once {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(15 * time.Second):
					continue
				}
			}

			currentAcc = nextAcc
			coord.SetPortAccount(ifaceCfg.Interface, currentAcc)
			logWorker(label, "成功切换为新账号: [%s] (套餐: %s, 过期时间: %s)，正在执行认证...",
				MaskString(currentAcc.Username, 3, 2), currentAcc.Package, currentAcc.ExpireStr)

			success, msg := Login(client, currentAcc, cfg, currentIP, currentMAC)
			if success {
				logWorker(label, "新账号 [%s] 认证成功: %s", MaskString(currentAcc.Username, 3, 2), msg)
				coord.UpdatePortStatus(ifaceCfg.Interface, true, currentIP, currentMAC)
				if isPrimary && cfg.SetTrafficPriority {
					_ = EnsureInterfaceMetric(ifaceCfg.Interface, 10)
				}
				portFailCount = 0
			} else {
				logWorker(label, "新账号 [%s] 认证失败: %s", MaskString(currentAcc.Username, 3, 2), msg)
				pool.MarkAccountFailed(currentAcc, msg)
			}

			if once {
				return
			}
			time.Sleep(3 * time.Second)
			continue
		}

		// 情况 C: 仅配置了单端口 (otherTotal == 0)
		if currentAcc == nil {
			currentAcc, _ = pool.AcquireNextUnusedAccount(ifaceCfg.Interface)
			coord.SetPortAccount(ifaceCfg.Interface, currentAcc)
		}

		if currentAcc != nil {
			if portFailCount > cfg.RetryMaxCount {
				logWorker(label, "当前账号 [%s] 连续失败达到上限(%d次)，切换到下一个未使用的账号...",
					MaskString(currentAcc.Username, 3, 2), cfg.RetryMaxCount)
				pool.MarkAccountFailed(currentAcc, "连续失败达上限")
				nextAcc, err := pool.AcquireNextUnusedAccount(ifaceCfg.Interface)
				if err == nil {
					currentAcc = nextAcc
					coord.SetPortAccount(ifaceCfg.Interface, currentAcc)
					portFailCount = 1
				}
			}

			logWorker(label, "正在进行第 %d 次认证尝试 (账号: %s)...", portFailCount, MaskString(currentAcc.Username, 3, 2))
			success, msg := Login(client, currentAcc, cfg, currentIP, currentMAC)
			if success {
				logWorker(label, "认证成功: %s", msg)
				coord.UpdatePortStatus(ifaceCfg.Interface, true, currentIP, currentMAC)
				portFailCount = 0
			} else {
				logWorker(label, "认证失败: %s", msg)
			}
		}

		if once {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(cfg.CheckInterval) * time.Second):
		}
	}
}
