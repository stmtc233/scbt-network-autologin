package main

import (
	"fmt"
	"sync"
	"time"
)

type AccountPool struct {
	mu       sync.Mutex
	accounts []*Account
}

func NewAccountPool(accounts []*Account) *AccountPool {
	return &AccountPool{
		accounts: accounts,
	}
}

// 获取或分配主账号给指定接口
func (p *AccountPool) AcquirePrimaryAccount(iface string) (*Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. 寻找被标记为 IsPrimary 的账号
	for _, acc := range p.accounts {
		if acc.IsPrimary && acc.Status != AccountStatusExpired {
			acc.Status = AccountStatusInUse
			acc.InUseBy = iface
			return acc, nil
		}
	}

	// 2. 如果未明确标记主账号，寻找第一个空闲账号
	for _, acc := range p.accounts {
		if acc.Status == AccountStatusIdle {
			acc.IsPrimary = true
			acc.Status = AccountStatusInUse
			acc.InUseBy = iface
			return acc, nil
		}
	}

	return nil, fmt.Errorf("账号池中无可用主账号")
}

// 为普通接口获取下一个未使用的空闲账号
func (p *AccountPool) AcquireNextUnusedAccount(iface string) (*Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. 优先寻找未被使用的空闲普通账号 (非主账号)
	for _, acc := range p.accounts {
		if !acc.IsPrimary && acc.Status == AccountStatusIdle {
			acc.Status = AccountStatusInUse
			acc.InUseBy = iface
			return acc, nil
		}
	}

	// 2. 如果没有普通空闲账号，检查失败账号中是否有已冷却超过10分钟的账号进行再尝试
	now := time.Now()
	var oldestFailed *Account
	for _, acc := range p.accounts {
		if !acc.IsPrimary && acc.Status == AccountStatusFailed {
			if now.Sub(acc.FailedAt) > 10*time.Minute {
				if oldestFailed == nil || acc.FailedAt.Before(oldestFailed.FailedAt) {
					oldestFailed = acc
				}
			}
		}
	}

	if oldestFailed != nil {
		oldestFailed.Status = AccountStatusInUse
		oldestFailed.InUseBy = iface
		return oldestFailed, nil
	}

	return nil, fmt.Errorf("账号池已耗尽，无空闲账号可用")
}

// 释放正在使用的账号回池
func (p *AccountPool) ReleaseAccount(acc *Account) {
	if acc == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if acc.Status == AccountStatusInUse {
		acc.Status = AccountStatusIdle
		acc.InUseBy = ""
	}
}

// 标记账号认证失败/失效
func (p *AccountPool) MarkAccountFailed(acc *Account, reason string) {
	if acc == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	acc.Status = AccountStatusFailed
	acc.FailedAt = time.Now()
	acc.FailReason = reason
	acc.InUseBy = ""
}

// 统计账号池状态
func (p *AccountPool) GetStats() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	total := len(p.accounts)
	inUse := 0
	idle := 0
	failed := 0
	expired := 0

	for _, acc := range p.accounts {
		switch acc.Status {
		case AccountStatusInUse:
			inUse++
		case AccountStatusIdle:
			idle++
		case AccountStatusFailed:
			failed++
		case AccountStatusExpired:
			expired++
		}
	}

	return fmt.Sprintf("总计: %d | 在线使用中: %d | 待用空闲: %d | 失败冷却: %d | 已过期: %d",
		total, inUse, idle, failed, expired)
}
