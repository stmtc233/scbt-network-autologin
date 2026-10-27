package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	flagConfig = flag.String("c", "", "指定配置文件路径 (默认先寻找当前目录 config.json，再回退 .env)")
	flagOnce   = flag.Bool("once", false, "单次执行模式：仅检查并登录一次，不常驻后台")
	flagInit   = flag.Bool("init", false, "在当前目录下生成多拨配置模板 config.json")
	flagManual = flag.Bool("manual", false, "纯手动模式：直接通过命令行参数登录单个接口")

	flagInterface = flag.String("i", "eth1", "手动模式: 网卡接口名称 (如 eth1, vwan1)")
	flagUsername  = flag.String("u", "", "手动模式: 校园网账号用户名")
	flagPassword  = flag.String("p", "", "手动模式: 校园网账号密码")
	flagMAC       = flag.String("mac", "", "手动模式: 接口MAC地址 (留空自动获取)")
	flagIP        = flag.String("ip", "", "手动模式: 接口IP地址 (留空自动获取)")
	flagBasIP     = flag.String("basip", "", "手动模式: BAS IP (留空使用默认)")
	flagNasID     = flag.String("nasid", "", "手动模式: NAS ID (留空使用默认)")
)

func runManualMode() {
	if *flagUsername == "" || *flagPassword == "" {
		fmt.Println("错误: 手动模式必须提供 -u (用户名) 和 -p (密码)")
		flag.Usage()
		os.Exit(1)
	}

	cfg := DefaultConfig()
	if *flagBasIP != "" {
		cfg.DefaultBasIP = *flagBasIP
	}
	if *flagNasID != "" {
		cfg.DefaultNasID = *flagNasID
	}

	iface := *flagInterface
	usrip := *flagIP
	if usrip == "" {
		usrip = GetIPv4ByInterface(iface)
	}
	if usrip == "" {
		log.Fatalf("错误: 无法从接口 %s 获取IPv4地址，请确认网卡已连接或通过 -ip 手动指定", iface)
	}

	usrmac := *flagMAC
	if usrmac == "" {
		usrmac = GetMACByInterface(iface)
	}

	log.Printf("[手动模式] 接口: %s | IP: %s | MAC: %s | 账号: %s", iface, usrip, usrmac, MaskString(*flagUsername, 3, 2))

	client := CreateClientForInterface(iface, net.ParseIP(usrip))
	t := true
	acc := &AccountConfig{
		Name:      iface,
		Interface: iface,
		Username:  *flagUsername,
		Password:  *flagPassword,
		MAC:       usrmac,
		IP:        usrip,
		BasIP:     *flagBasIP,
		NasID:     *flagNasID,
		Enabled:   &t,
	}

	success, msg := Login(client, acc, cfg, usrip, usrmac)
	if success {
		log.Printf("[手动模式] 登录结果: 成功! (%s)", msg)
	} else {
		log.Fatalf("[手动模式] 登录结果: 失败! (%s)", msg)
	}
}

func main() {
	flag.Parse()

	log.SetFlags(log.Ldate | log.Ltime)

	// 1. 初始化模板
	if *flagInit {
		if _, err := os.Stat("config.json"); err == nil {
			fmt.Println("config.json 文件已存在，避免覆盖未执行操作。")
			return
		}
		cfg := DefaultConfig()
		if err := SaveConfig("config.json", cfg); err != nil {
			log.Fatalf("生成配置文件失败: %v", err)
		}
		fmt.Println("已生成多拨配置文件模板: config.json")
		fmt.Println("请编辑该文件填入你的多拨接口、账号和密码后再次运行。")
		return
	}

	// 2. 命令行手动单次模式
	if *flagManual {
		runManualMode()
		return
	}

	// 3. 读取配置文件 (优先 config.json / -c，其次 .env)
	cfg, err := LoadConfig(*flagConfig)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	activeAccounts := make([]AccountConfig, 0)
	for _, acc := range cfg.Accounts {
		if acc.IsEnabled() {
			activeAccounts = append(activeAccounts, acc)
		}
	}

	if len(activeAccounts) == 0 {
		log.Fatalf("错误: 配置文件中没有任何已启用的账号！")
	}

	log.Printf("================ 校园网多拨自动认证程序 ================")
	log.Printf("共加载 %d 个有效多拨账号配置，检测周期: %d 秒，重试上限: %d 次",
		len(activeAccounts), cfg.CheckInterval, cfg.RetryMaxCount)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("接收到退出信号 (%v)，正在停止所有接口监控...", sig)
		cancel()
	}()

	var wg sync.WaitGroup
	for _, acc := range activeAccounts {
		wg.Add(1)
		account := acc
		go func() {
			defer wg.Done()
			RunAccountWorker(ctx, account, cfg, *flagOnce)
		}()
	}

	wg.Wait()
	log.Printf("所有认证任务已退出。")
}
