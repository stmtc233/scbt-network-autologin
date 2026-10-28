package main

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
)

func IsOnline(client *resty.Client, testURL string) bool {
	if testURL == "" {
		testURL = DefaultTestURL
	}
	resp, err := client.R().Get(testURL)
	if err != nil {
		return false
	}
	return resp.StatusCode() == 200 && strings.Contains(string(resp.Body()), "baidu")
}

func parseTokenFromHtml(html []byte) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return ""
	}
	token, exists := doc.Find("input[name='cmcc_login_value']").Attr("value")
	if !exists {
		return ""
	}
	return strings.TrimSpace(token)
}

func postLoginRaw(client *resty.Client, username, password, basip, nasid string, cfg *Config, usrip, usrmac string) (string, error) {
	if basip == "" {
		basip = cfg.DefaultBasIP
	}
	if nasid == "" {
		nasid = cfg.DefaultNasID
	}

	formData := map[string]string{
		"usrname":        username,
		"passwd":         password,
		"treaty":         "on",
		"nasid":          nasid,
		"usrmac":         usrmac,
		"usrip":          usrip,
		"basip":          basip,
		"success":        cfg.SuccessURL,
		"fail":           cfg.FailURL,
		"offline":        "1",
		"portal_version": "1",
		"portal_papchap": "pap",
	}

	resp, err := client.R().SetFormData(formData).Post(cfg.LoginURL)
	if err != nil {
		return "", fmt.Errorf("POST login 网络请求错误: %w", err)
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("POST login 返回非200状态码: %d", resp.StatusCode())
	}

	token := parseTokenFromHtml(resp.Body())
	if token == "" {
		bodyStr := string(resp.Body())
		if strings.Contains(bodyStr, "登录成功") || strings.Contains(bodyStr, "在线") {
			return "ALREADY_ONLINE", nil
		}
		// 截取错误提示片段
		cleanBody := strings.Join(strings.Fields(bodyStr), " ")
		if len(cleanBody) > 100 {
			cleanBody = cleanBody[:100] + "..."
		}
		return "", fmt.Errorf("未在返回页面中找到登录Token (响应片段: %s)", cleanBody)
	}

	return token, nil
}

func postToken(client *resty.Client, token string, loginURL string) bool {
	resp, err := client.R().SetFormData(map[string]string{
		"cmcc_login_value": token,
	}).Post(loginURL)
	if err != nil {
		return false
	}
	return resp.StatusCode() == 200
}

func checkStatus(client *resty.Client, successURL string) bool {
	resp, err := client.R().Get(successURL)
	if err != nil {
		return false
	}
	return resp.StatusCode() == 200 && strings.Contains(string(resp.Body()), "登录成功")
}

func LoginAccount(client *resty.Client, username, password, basip, nasid string, cfg *Config, usrip, usrmac string) (bool, string) {
	// 步骤 1: 提交凭据，获取 Token
	token, err := postLoginRaw(client, username, password, basip, nasid, cfg, usrip, usrmac)
	if err != nil {
		return false, fmt.Sprintf("步骤1失败: %v", err)
	}
	if token == "ALREADY_ONLINE" {
		return true, "账号检测到已处于在线状态"
	}

	// 步骤 2: 提交 Token
	if !postToken(client, token, cfg.LoginURL) {
		return false, "步骤2失败: 提交Token失败"
	}

	// 稍作停顿让 RADIUS / BAS 状态生效
	time.Sleep(500 * time.Millisecond)

	// 步骤 4: 检查最终登录状态页面
	if !checkStatus(client, cfg.SuccessURL) {
		// 容错：如果最终状态页未返回“登录成功”，用网络测试兜底确认
		if IsOnline(client, cfg.TestURL) {
			return true, "登录成功 (外网连通性验证通过)"
		}
		return false, "步骤4失败: 成功页面状态校验未通过"
	}

	return true, "登录成功"
}

func Login(client *resty.Client, acc *Account, cfg *Config, usrip, usrmac string) (bool, string) {
	return LoginAccount(client, acc.Username, acc.Password, cfg.DefaultBasIP, cfg.DefaultNasID, cfg, usrip, usrmac)
}

func LoginLegacy(client *resty.Client, acc *AccountConfig, cfg *Config, usrip, usrmac string) (bool, string) {
	basip := acc.BasIP
	if basip == "" {
		basip = cfg.DefaultBasIP
	}
	nasid := acc.NasID
	if nasid == "" {
		nasid = cfg.DefaultNasID
	}
	return LoginAccount(client, acc.Username, acc.Password, basip, nasid, cfg, usrip, usrmac)
}
