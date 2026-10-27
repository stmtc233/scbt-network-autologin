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
)

func DefaultConfig() *Config {
	t := true
	return &Config{
		CheckInterval: 15,
		RetryMaxCount: 5,
		TestURL:       DefaultTestURL,
		LoginURL:      DefaultLoginURL,
		SuccessURL:    DefaultSuccessURL,
		FailURL:       DefaultFailURL,
		DefaultBasIP:  DefaultBasIP,
		DefaultNasID:  DefaultNasID,
		Accounts: []AccountConfig{
			{
				Name:      "wan1",
				Interface: "eth1",
				Username:  "13000000000",
				Password:  "password123",
				MAC:       "",
				IP:        "",
				BasIP:     "",
				NasID:     "",
				Enabled:   &t,
			},
			{
				Name:      "wan2",
				Interface: "vwan1",
				Username:  "13800000000",
				Password:  "password456",
				MAC:       "",
				IP:        "",
				BasIP:     "",
				NasID:     "",
				Enabled:   &t,
			},
		},
	}
}

func LoadConfig(configPath string) (*Config, error) {
	// 1. 如果指定了配置文件，或存在 config.json
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
		cfg.Accounts = nil // 清空默认模板账号
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s JSON失败: %w", targetPath, err)
		}
		fillConfigDefaults(cfg)
		return cfg, nil
	}

	// 2. 如果存在 .env 文件，回退到老版本兼容模式 (单拨模式)
	if _, err := os.Stat(".env"); err == nil {
		_ = godotenv.Load()
		cfg := DefaultConfig()
		cfg.Accounts = nil

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
			iface = "eth0"
		}
		t := true
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
		cfg.RetryMaxCount = 5
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
}

func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
