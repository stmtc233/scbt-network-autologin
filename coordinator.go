package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type PortState struct {
	Interface      string
	Name           string
	IsPrimary      bool
	CurrentAccount *Account
	IsOnline       bool
	LastOnlineTime time.Time
	LastCheckTime  time.Time
	FailCount      int
	CurrentIP      string
	CurrentMAC     string
}

type Coordinator struct {
	mu            sync.RWMutex
	ports         map[string]*PortState
	checkInterval int
}

func NewCoordinator(checkInterval int) *Coordinator {
	if checkInterval <= 0 {
		checkInterval = 15
	}
	return &Coordinator{
		ports:         make(map[string]*PortState),
		checkInterval: checkInterval,
	}
}

func (c *Coordinator) RegisterPort(iface string, name string, isPrimary bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ports[iface] = &PortState{
		Interface: iface,
		Name:      name,
		IsPrimary: isPrimary,
	}
}

func (c *Coordinator) SetPortAccount(iface string, acc *Account) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if p, ok := c.ports[iface]; ok {
		p.CurrentAccount = acc
	}
}

func (c *Coordinator) GetPortAccount(iface string) *Account {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if p, ok := c.ports[iface]; ok {
		return p.CurrentAccount
	}
	return nil
}

func (c *Coordinator) UpdatePortStatus(iface string, online bool, ip string, mac string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	p, ok := c.ports[iface]
	if !ok {
		return
	}

	p.IsOnline = online
	p.LastCheckTime = time.Now()
	p.CurrentIP = ip
	p.CurrentMAC = mac

	if online {
		p.LastOnlineTime = time.Now()
		p.FailCount = 0
	} else {
		p.FailCount++
	}
}

// 检查除了指定接口外的其他端口存活状态
// 返回: otherTotal (其他接口总数), otherOnline (其他接口中当前在线的数量)
func (c *Coordinator) GetOtherPortsStatus(currentIface string) (otherTotal int, otherOnline int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 允许的最大在线心跳超时时间 (比如 2.5 倍检测周期)
	onlineThreshold := time.Duration(c.checkInterval*2500) * time.Millisecond
	now := time.Now()

	for iface, p := range c.ports {
		if iface == currentIface {
			continue
		}
		otherTotal++
		// 必须满足状态为 online 且最近心跳在阈值内
		if p.IsOnline && (now.Sub(p.LastOnlineTime) <= onlineThreshold) {
			otherOnline++
		}
	}

	return otherTotal, otherOnline
}

func (c *Coordinator) GetPortSummary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var parts []string
	for iface, p := range c.ports {
		statusStr := "离线"
		if p.IsOnline {
			statusStr = "在线"
		}
		userStr := "无账号"
		if p.CurrentAccount != nil {
			userStr = MaskString(p.CurrentAccount.Username, 3, 2)
		}
		primaryTag := ""
		if p.IsPrimary {
			primaryTag = "(主)"
		}
		parts = append(parts, fmt.Sprintf("%s%s[%s]: %s", iface, primaryTag, userStr, statusStr))
	}
	return strings.Join(parts, " | ")
}
