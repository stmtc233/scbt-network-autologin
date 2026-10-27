package main

type AccountConfig struct {
	Name      string `json:"name,omitempty"`      // 账号别名/标签 (如 wan1, wan2)
	Interface string `json:"interface"`           // 绑定的网卡接口名 (如 eth1, vwan1)
	Username  string `json:"username"`            // 校园网登录用户名
	Password  string `json:"password"`            // 校园网登录密码
	MAC       string `json:"mac,omitempty"`       // 手动指定MAC地址 (留空则自动读取网卡真实MAC)
	IP        string `json:"ip,omitempty"`        // 手动指定IP地址 (留空则自动读取网卡当前IPv4)
	BasIP     string `json:"basip,omitempty"`     // 手动指定BAS IP (留空则使用全局默认basip)
	NasID     string `json:"nasid,omitempty"`     // 手动指定NAS ID (留空则使用全局默认nasid)
	Enabled   *bool  `json:"enabled,omitempty"`   // 是否启用 (默认true)
}

func (a *AccountConfig) IsEnabled() bool {
	if a.Enabled == nil {
		return true
	}
	return *a.Enabled
}

type Config struct {
	CheckInterval int             `json:"check_interval"` // 检测间隔 (秒，默认15)
	RetryMaxCount int             `json:"retry_maxcount"` // 连续失败最大重试次数 (默认5)
	TestURL       string          `json:"test_url"`       // 连通性测试网址 (默认 https://www.baidu.com/)
	LoginURL      string          `json:"login_url"`      // 认证接口URL
	SuccessURL    string          `json:"success_url"`    // 成功页面URL
	FailURL       string          `json:"fail_url"`       // 失败页面URL
	DefaultBasIP  string          `json:"default_basip"`  // 默认BAS IP
	DefaultNasID  string          `json:"default_nasid"`  // 默认NAS ID
	Accounts      []AccountConfig `json:"accounts"`       // 多拨多账号列表
}
