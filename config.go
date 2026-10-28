package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

const (
	DefaultLoginURL   = "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/cmcc_login/"
	DefaultSuccessURL = "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/success/"
	DefaultFailURL    = "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/fail/"
	DefaultTestURL    = "https://www.baidu.com/"
	DefaultBasIP      = "10.241.0.11"
	DefaultNasID      = "4"
	DefaultCSVFile    = "accounts.csv"
)

func DefaultConfig() *Config {
	t := true
	f := false
	return &Config{
		CSVFile:            DefaultCSVFile,
		PrimaryAccount:     "255102010828",
		PrimaryInterface:   "eth1",
		SetTrafficPriority: true,
		CheckInterval:      15,
		RetryMaxCount:      3,
		TestURL:            DefaultTestURL,
		LoginURL:           DefaultLoginURL,
		SuccessURL:         DefaultSuccessURL,
		FailURL:            DefaultFailURL,
		DefaultBasIP:       DefaultBasIP,
		DefaultNasID:       DefaultNasID,
		Interfaces: []InterfaceConfig{
			{
				Name:      "主WAN",
				Interface: "eth1",
				IsPrimary: true,
				Enabled:   &t,
			},
			{
				Name:      "副WAN1",
				Interface: "vwan1",
				IsPrimary: false,
				Enabled:   &f,
			},
		},
	}
}

func LoadConfig(configPath string) (*Config, error) {
	targetPath := configPath
	if targetPath == "" {
		if _, err := os.Stat("config.json"); err == nil {
			targetPath = "config.json"
		}
	}

	if targetPath != "" {
		data, err := os.ReadFile(targetPath)
		if err != nil {
			return nil, fmt.Errorf("读取配置文件 %s 失败: %w", targetPath, err)
		}
		cfg := DefaultConfig()
		cfg.Interfaces = nil
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s JSON失败: %w", targetPath, err)
		}
		fillConfigDefaults(cfg)

		// 兼容旧版单一/多拨配置格式 (从 accounts 转换为 interfaces)
		if len(cfg.Interfaces) == 0 && len(cfg.Accounts) > 0 {
			t := true
			for _, acc := range cfg.Accounts {
				cfg.Interfaces = append(cfg.Interfaces, InterfaceConfig{
					Name:      acc.Name,
					Interface: acc.Interface,
					MAC:       acc.MAC,
					IP:        acc.IP,
					IsPrimary: acc.Interface == cfg.PrimaryInterface,
					Enabled:   &t,
				})
			}
		}

		return cfg, nil
	}

	// 回退兼容 .env
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load()
		cfg := DefaultConfig()
		cfg.Interfaces = nil

		checkInterval, err := strconv.Atoi(os.Getenv("CHECK_INTERVAL"))
		if err == nil && checkInterval > 0 {
			cfg.CheckInterval = checkInterval
		}
		retryMaxCount, err := strconv.Atoi(os.Getenv("RETRY_MAXCOUNT"))
		if err == nil && retryMaxCount > 0 {
			cfg.RetryMaxCount = retryMaxCount
		}

		iface := os.Getenv("INTERFACE_NAME")
		if iface == "" {
			iface = "eth1"
		}
		cfg.PrimaryInterface = iface
		cfg.PrimaryAccount = os.Getenv("USER_ID")

		t := true
		cfg.Interfaces = append(cfg.Interfaces, InterfaceConfig{
			Name:      iface,
			Interface: iface,
			MAC:       os.Getenv("DEVICE_MAC"),
			IP:        os.Getenv("USER_IP"),
			IsPrimary: true,
			Enabled:   &t,
		})

		cfg.Accounts = append(cfg.Accounts, AccountConfig{
			Name:      iface,
			Interface: iface,
			Username:  os.Getenv("USER_ID"),
			Password:  os.Getenv("PASSWORD"),
			MAC:       os.Getenv("DEVICE_MAC"),
			IP:        os.Getenv("USER_IP"),
			Enabled:   &t,
		})

		fillConfigDefaults(cfg)
		return cfg, nil
	}

	return nil, fmt.Errorf("未找到 config.json 或 .env 配置文件，请使用 -init 生成配置文件模板，或使用 -c 指定路径")
}

func fillConfigDefaults(cfg *Config) {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 15
	}
	if cfg.RetryMaxCount <= 0 {
		cfg.RetryMaxCount = 3
	}
	if cfg.TestURL == "" {
		cfg.TestURL = DefaultTestURL
	}
	if cfg.LoginURL == "" {
		cfg.LoginURL = DefaultLoginURL
	}
	if cfg.SuccessURL == "" {
		cfg.SuccessURL = DefaultSuccessURL
	}
	if cfg.FailURL == "" {
		cfg.FailURL = DefaultFailURL
	}
	if cfg.DefaultBasIP == "" {
		cfg.DefaultBasIP = DefaultBasIP
	}
	if cfg.DefaultNasID == "" {
		cfg.DefaultNasID = DefaultNasID
	}
	if cfg.PrimaryInterface == "" && len(cfg.Interfaces) > 0 {
		cfg.PrimaryInterface = cfg.Interfaces[0].Interface
		cfg.Interfaces[0].IsPrimary = true
	}
}

func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func InitTemplates() error {
	// 1. 生成 accounts.csv
	if _, err := os.Stat("accounts.csv"); os.IsNotExist(err) {
		sampleCSV := `"学号","密码","套餐","过期时间"
"255102010828","094217","100M包学期2终端-学生","2030-10-31 00:00:00"
"255602130329","257607","100M包学期2终端-学生","2030-10-31 00:00:00"
`
		if err := os.WriteFile("accounts.csv", []byte(sampleCSV), 0644); err != nil {
			return fmt.Errorf("生成 accounts.csv 失败: %w", err)
		}
		fmt.Println("已生成账号池模板文件: accounts.csv")
	} else {
		fmt.Println("accounts.csv 文件已存在，跳过生成。")
	}

	// 2. 生成 config.json
	if _, err := os.Stat("config.json"); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := SaveConfig("config.json", cfg); err != nil {
			return fmt.Errorf("生成 config.json 失败: %w", err)
		}
		fmt.Println("已生成多拨配置文件模板: config.json")
	} else {
		fmt.Println("config.json 文件已存在，跳过生成。")
	}

	return nil
}
