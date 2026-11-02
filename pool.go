package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type AccountPool struct {
	mu        sync.Mutex
	csvPath   string
	csvHeader []string
	accounts  []*Account
}

func NewAccountPool(csvPath string, csvHeader []string, accounts []*Account) *AccountPool {
	return &AccountPool{
		csvPath:   csvPath,
		csvHeader: csvHeader,
		accounts:  accounts,
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

// 彻底删除失效账号（从内存和CSV文件中删除）
func (p *AccountPool) RemoveInvalidAccount(acc *Account, reason string) {
	if acc == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// 保护主账号不被删除
	if acc.IsPrimary {
		log.Printf("[AccountPool] 警告: 主账号 [%s] 认证异常 (%s)，受保护不从列表删除",
			MaskString(acc.Username, 3, 2), reason)
		acc.Status = AccountStatusFailed
		acc.FailedAt = time.Now()
		acc.FailReason = reason
		acc.InUseBy = ""
		return
	}

	log.Printf("[AccountPool] 账号 [%s] 已判定失效 (%s)，正在从账号池及文件中彻底删除...",
		MaskString(acc.Username, 3, 2), reason)

	// 1. 从内存切片中移除
	newAccounts := make([]*Account, 0, len(p.accounts))
	for _, a := range p.accounts {
		if a.Username != acc.Username {
			newAccounts = append(newAccounts, a)
		}
	}
	p.accounts = newAccounts

	// 2. 同步保存回 CSV 文件
	if p.csvPath != "" {
		if err := p.saveCSVLocked(); err != nil {
			log.Printf("[AccountPool] 同步写回 CSV 文件 %s 失败: %v", p.csvPath, err)
		} else {
			log.Printf("[AccountPool] 成功从 %s 移除失效账号 [%s]，池中剩余有效账号: %d",
				p.csvPath, MaskString(acc.Username, 3, 2), len(p.accounts))
		}
	}
}

// 内部保存 CSV 文件 (调用前必须持有 p.mu)
func (p *AccountPool) saveCSVLocked() error {
	if p.csvPath == "" {
		return nil
	}

	tmpFile := p.csvPath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}

	w := csv.NewWriter(f)
	if len(p.csvHeader) > 0 {
		if err := w.Write(p.csvHeader); err != nil {
			f.Close()
			return fmt.Errorf("写入CSV表头失败: %w", err)
		}
	}

	for _, acc := range p.accounts {
		if len(acc.RawRow) > 0 {
			if err := w.Write(acc.RawRow); err != nil {
				f.Close()
				return fmt.Errorf("写入CSV行失败: %w", err)
			}
		} else {
			row := []string{acc.Username, acc.Password, acc.Package, acc.ExpireStr}
			if err := w.Write(row); err != nil {
				f.Close()
				return fmt.Errorf("写入CSV行失败: %w", err)
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return fmt.Errorf("刷新CSV流失败: %w", err)
	}
	f.Close()

	if err := os.Rename(tmpFile, p.csvPath); err != nil {
		return fmt.Errorf("替换CSV文件失败: %w", err)
	}

	return nil
}

// 标记账号认证失败/失效
func (p *AccountPool) MarkAccountFailed(acc *Account, reason string) {
	if acc == nil {
		return
	}
	p.RemoveInvalidAccount(acc, reason)
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
