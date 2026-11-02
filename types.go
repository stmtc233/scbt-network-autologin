package main

import (
	"time"
)

type AccountStatus string

const (
	AccountStatusIdle    AccountStatus = "idle"    // 空闲未分配
	AccountStatusInUse   AccountStatus = "in_use"  // 正在某网卡使用中
	AccountStatusFailed  AccountStatus = "failed"  // 认证失败/被踢下线
	AccountStatusExpired AccountStatus = "expired" // 已过期
)

type Account struct {
	Username   string        `json:"username"`
	Password   string        `json:"password"`
	Package    string        `json:"package,omitempty"`
	ExpireStr  string        `json:"expire_str,omitempty"`
	ExpireTime time.Time     `json:"expire_time,omitempty"`
	IsPrimary  bool          `json:"is_primary"`
	Status     AccountStatus `json:"status"`
	FailedAt   time.Time     `json:"failed_at,omitempty"`
	FailReason string        `json:"fail_reason,omitempty"`
	InUseBy    string        `json:"in_use_by,omitempty"` // 正在使用此账号的接口名 (如 eth1)
	RawRow     []string      `json:"-"`
}

type InterfaceConfig struct {
	Name      string `json:"name,omitempty"`      // 接口别名 (如 wan1, wan2)
	Interface string `json:"interface"`           // 网卡接口名称 (如 eth1, vwan1)
	MAC       string `json:"mac,omitempty"`       // 手动指定MAC (留空自动获取)
	IP        string `json:"ip,omitempty"`        // 手动指定IP (留空自动获取)
	IsPrimary bool   `json:"is_primary,omitempty"`// 是否为主接口 (流量优先通过此接口)
	Enabled   *bool  `json:"enabled,omitempty"`   // 是否启用 (默认true)
}

func (i *InterfaceConfig) IsEnabled() bool {
	if i.Enabled == nil {
		return true
	}
	return *i.Enabled
}

// 兼容旧版单一账号配置
type AccountConfig struct {
	Name      string `json:"name,omitempty"`
	Interface string `json:"interface"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	MAC       string `json:"mac,omitempty"`
	IP        string `json:"ip,omitempty"`
	BasIP     string `json:"basip,omitempty"`
	NasID     string `json:"nasid,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
}

func (a *AccountConfig) IsEnabled() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

type Config struct {
	CSVFile            string            `json:"csv_file"`              // 账号CSV文件路径 (如 accounts.csv)
	PrimaryAccount     string            `json:"primary_account"`       // 主账号学号/用户名
	PrimaryInterface   string            `json:"primary_interface"`     // 主端口名称 (如 eth1)
	SetTrafficPriority bool              `json:"set_traffic_priority"`  // 是否设置路由跃点使得主端口优先
	CheckInterval      int               `json:"check_interval"`        // 检测间隔 (秒，默认15)
	RetryMaxCount      int               `json:"retry_maxcount"`        // 连续重试上限 (默认3)
	TestURL            string            `json:"test_url"`              // 连通性测试网址
	LoginURL           string            `json:"login_url"`             // 认证接口URL
	SuccessURL         string            `json:"success_url"`           // 成功页面URL
	FailURL            string            `json:"fail_url"`              // 失败页面URL
	DefaultBasIP       string            `json:"default_basip"`         // 默认BAS IP
	DefaultNasID       string            `json:"default_nasid"`         // 默认NAS ID
	Interfaces         []InterfaceConfig `json:"interfaces"`            // 多拨网卡/接口列表
	Accounts           []AccountConfig   `json:"accounts,omitempty"`    // 旧版兼容账号列表
}
