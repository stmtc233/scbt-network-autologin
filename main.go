package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/joho/godotenv"
)

var GETIP_URL = "http://www.qq.com/"
var TEST_URL = "https://www.baidu.com/"

// var LOGIN_URL = "http://p.0086-10010.com:9770/lfradius/libs/portal/unify/portal.php/login/cmcc_login/"
// var ONSUCCESS_URL = "http://p.0086-10010.com:9770/lfradius/libs/portal/unify/portal.php/login/success/"
// var ONFAIL_URL = "http://p.0086-10010.com:9770/lfradius/libs/portal/unify/portal.php/login/fail/"
// var CHECKSTATUS_URL = "http://p.0086-10010.com:9770/lfradius/libs/portal/unify/portal.php/login/cmcc_login_result/"
var LOGIN_URL = "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/cmcc_login/"
var ONSUCCESS_URL = "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/success/"
var ONFAIL_URL = "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/fail/"

func isOnline() bool {
	client := resty.New()
	client.SetTimeout(5 * time.Second)
	// 检查网络连通性时不走代理
	client.RemoveProxy()
	resp, err := client.R().Get(TEST_URL)
	if err != nil {
		return false
	}
	// 检查返回内容，防止被重定向到登录页
	return resp.StatusCode() == 200 && strings.Contains(string(resp.Body()), "baidu")
}

// 修改后的Token解析函数，通过name属性更精确地查找
func parseTokenFromHtml(html []byte) string {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return ""
	}
	// 通过 input 的 name 属性来查找，更准确
	token, exists := doc.Find("input[name='cmcc_login_value']").Attr("value")
	if !exists {
		return ""
	}
	return token
}

func GetIPv4ByInterface(name string) string {
	// 根据名称获取网络接口
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}

	// 获取该接口的所有地址
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}

	// 遍历所有地址
	for _, addr := range addrs {
		var ip net.IP
		// 进行类型断言，判断地址是否为 IPNet 类型
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			// 检查是否为IPv4地址
			if ipNet.IP.To4() != nil {
				ip = ipNet.IP
			}
		}

		if ip != nil {
			return ip.String()
		}
	}

	return ""
}

// 第1步：提交用户名和密码，获取包含token的HTML和Cookies
func postLogin(client *resty.Client, usrip string) string {
	log.Println(map[string]string{
		"usrname":        os.Getenv("USER_ID"),
		"passwd":         os.Getenv("PASSWORD"),
		"treaty":         "on",
		"nasid":          "4",
		"usrmac":         os.Getenv("USER_MAC"), // 从环境变量读取MAC地址
		"usrip":          usrip,
		"basip":          "10.241.0.11",
		"success":        ONSUCCESS_URL,
		"fail":           ONFAIL_URL,
		"offline":        "1",
		"portal_version": "1",
		"portal_papchap": "pap",
	})
	resp, err := client.R().
		SetFormData(map[string]string{
			"usrname":        os.Getenv("USER_ID"),
			"passwd":         os.Getenv("PASSWORD"),
			"treaty":         "on",
			"nasid":          "4",
			"usrmac":         os.Getenv("USER_MAC"), // 从环境变量读取MAC地址
			"usrip":          usrip,
			"basip":          "10.241.0.11",
			"success":        ONSUCCESS_URL,
			"fail":           ONFAIL_URL,
			"offline":        "1",
			"portal_version": "1",
			"portal_papchap": "pap",
		}).Post(LOGIN_URL)

	if err != nil {
		log.Println("postLogin request error:", err)
		return ""
	}

	if resp.StatusCode() == 200 {
		// Cookies 会被自动保存在 client 的 CookieJar 中
		// 现在从返回的HTML中解析出 token
		return parseTokenFromHtml(resp.Body())
	} else {
		log.Printf("postLogin status code: %d", resp.StatusCode())
		return ""
	}
}

// 第2步：提交上一步获取到的token
// 此时 client 已经自动带上了第1步获取的 Cookies
func postToken(client *resty.Client, token string) bool {
	resp, err := client.R().SetFormData(map[string]string{
		"cmcc_login_value": token,
	}).Post(LOGIN_URL) // 还是请求到同一个LOGIN_URL

	if err != nil {
		log.Println("postToken request error:", err)
		return false
	}
	// 根据你的描述，这一步成功后会返回一个包含JS轮询的页面
	return resp.StatusCode() == 200
}

// 第3步：轮询检查登录状态
// func getStatus(client *resty.Client, token string) bool {
// 	// 轮询最多10次，每次间隔2秒
// 	for i := 0; i < 10; i++ {
// 		resp, err := client.R().
// 			SetFormData(map[string]string{
// 				// 注意，这里的参数名是 "l"
// 				"l": token,
// 			}).Post(CHECKSTATUS_URL)

// 		if err != nil {
// 			log.Println("getStatus request error:", err)
// 			// 网络错误，稍后重试
// 			time.Sleep(2 * time.Second)
// 			continue
// 		}

// 		if resp.StatusCode() == 200 && string(resp.Body()) == "success" {
// 			return true
// 		}
// 		// 等待2秒再进行下一次查询
// 		time.Sleep(2 * time.Second)
// 	}
// 	return false
// }

// 第4步：最终确认登录成功页面
func checkStatus(client *resty.Client) bool {
	resp, err := client.R().Get(ONSUCCESS_URL)
	if err != nil {
		log.Println("checkStatus request error:", err)
		return false
	}
	// 检查状态码并且页面内容是否包含“登录成功”
	return resp.StatusCode() == 200 && strings.Contains(string(resp.Body()), "登录成功")
}

// 整合的登录流程
func login() (bool, string) {
	// 创建一个 resty 客户端并启用 CookieJar 来自动管理 Cookies
	jar, _ := cookiejar.New(nil)
	client := resty.New().SetCookieJar(jar)

	usrip := GetIPv4ByInterface(os.Getenv("INTERFACE_NAME"))
	if usrip == "" {
		// 如果无法自动获取IP，可以尝试使用环境变量中的备用IP
		usrip = os.Getenv("USER_IP")
		if usrip == "" {
			return false, "无法获取用户IP，请在.env文件中配置USER_IP。"
		}
		printLog(fmt.Sprintf("无法自动获取IP，使用预设IP: %s", usrip))
	} else {
		printLog(fmt.Sprintf("获取到当前IP: %s", usrip))
	}

	// 第1步：提交凭据，获取token
	token := postLogin(client, usrip)
	if token == "" {
		return false, "步骤1失败: 无法正确获取Token。"
	}
	printLog("步骤1成功: 获取Token完成。")

	// 第2步：提交token
	if !postToken(client, token) {
		return false, "步骤2失败: 无法提交登录状态。"
	}
	printLog("步骤2成功: 提交Token完成。")

	// 第3步：轮询状态
	// if !getStatus(client, token) {
	// 	return false, "步骤3失败: 查询登录状态失败。"
	// }
	// printLog("步骤3成功: 查询登录状态成功。")

	// 第4步：检查最终成功页面
	if !checkStatus(client) {
		return false, "步骤4失败: 最终状态检查失败。"
	}
	printLog("步骤4成功: 登录成功页面确认。")

	return true, "登录成功."
}

func printLog(logStr string) {
	log.Printf("%s | %s", time.Now().Format("2006-01-02 15:04:05"), logStr)
}

func mainLoop() {
	checkInterval, err := strconv.ParseInt(os.Getenv("CHECK_INTERVAL"), 10, 64)
	if err != nil {
		checkInterval = 30 // 建议检测间隔长一些，比如30秒
	}
	retryMaxCount, err := strconv.ParseInt(os.Getenv("RETRY_MAXCOUNT"), 10, 64)
	if err != nil {
		retryMaxCount = 5
	}
	printLog("校园网自动认证程序已启动。")

	// 启动时先检查一次网络状态
	if isOnline() {
		printLog("网络已在线，进入监控模式。")
	} else {
		printLog("网络离线，立即尝试登录。")
		res, hint := login()
		printLog(hint)
		if res {
			printLog("登录成功，进入监控模式。")
		} else {
			printLog("首次登录失败，将在后台继续尝试。")
		}
	}

	count := 0
	for {
		// 每次循环都等待指定的时间
		time.Sleep(time.Duration(checkInterval) * time.Second)

		if isOnline() {
			// 如果在线，重置重试计数器
			count = 0
			// printLog("网络在线，状态正常。") // 可以取消这行注释来获得心跳日志
			continue
		}

		if count >= int(retryMaxCount) {
			printLog(fmt.Sprintf("已达到最大重试次数(%d)，程序暂停10分钟后继续。", retryMaxCount))
			time.Sleep(10 * time.Minute)
			count = 0 // 暂停后重置计数器
			continue
		}

		printLog(fmt.Sprintf("网络离线，正在进行第%d次重连...", count+1))
		res, hint := login()
		printLog(hint)

		if res {
			count = 0 // 登录成功后重置计数器
			printLog("重连成功！")
		} else {
			count++
			printLog("重连失败。")
		}
	}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		printLog("未找到.env文件，请确保环境变量已设置。")
	} else {
		printLog("已加载.env文件到环境变量中。")
	}
	log.Println(GetIPv4ByInterface(os.Getenv("INTERFACE_NAME")))
	mainLoop()
}
