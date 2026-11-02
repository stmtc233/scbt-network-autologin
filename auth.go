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
	body := string(resp.Body())

	// 如果包含校园网 Portal 拦截页面或691欠费页面特征，坚决判定为未登录/离线
	if strings.Contains(body, "Web认证") ||
		strings.Contains(body, "0086-10010.com") ||
		strings.Contains(body, "wlanacip") ||
		strings.Contains(body, "wlanacname") ||
		strings.Contains(body, "cmcc_login") ||
		strings.Contains(body, "lfradius") ||
		strings.Contains(body, "691.gd165.com") ||
		strings.Contains(body, "691错误") ||
		strings.Contains(body, "callForErrByName") ||
		strings.Contains(body, "停机状态") ||
		strings.Contains(body, "error_arrears") {
		return false
	}

	// 真实连通性特征：响应正常且包含百度真实特征 (防止将包含 url=baidu 的重定向页误认为在线)
	if resp.StatusCode() == 200 && (strings.Contains(body, "百度一下") || strings.Contains(body, "hao123") || len(body) > 1000) {
		return true
	}

	return false
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
		if IsOnline(client, cfg.TestURL) {
			return true, "账号检测到已处于在线状态且外网验证通过"
		}
		return false, "账号在其他终端在线(并发受限)，本机未获得外网放行"
	}

	// 步骤 2: 提交 Token
	if !postToken(client, token, cfg.LoginURL) {
		return false, "步骤2失败: 提交Token失败"
	}

	// 稍作停顿让 RADIUS / BAS 状态生效
	time.Sleep(1 * time.Second)

	// 步骤 3: 严格真实外网连通性验证 (防止Portal虚假成功或欠费691拦截)
	verified := false
	for i := 0; i < 5; i++ {
		if IsOnline(client, cfg.TestURL) {
			verified = true
			break
		}
		time.Sleep(1 * time.Second)
	}

	if !verified {
		return false, "Portal认证通过但外网测试未通 (可能宽带欠费停机691或BAS未放行)"
	}

	return true, "登录成功 (外网连通性验证通过)"
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
