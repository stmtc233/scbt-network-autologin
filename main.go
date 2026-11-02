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
	flagCSV    = flag.String("csv", "", "指定账号CSV文件路径 (覆盖 config.json 中的 csv_file)")
	flagOnce   = flag.Bool("once", false, "单次执行模式：仅检查并登录一次，不常驻后台")
	flagInit   = flag.Bool("init", false, "生成多拨配置模板 config.json 与账号池模板 accounts.csv")
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
	success, msg := LoginAccount(client, *flagUsername, *flagPassword, *flagBasIP, *flagNasID, cfg, usrip, usrmac)
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
		if err := InitTemplates(); err != nil {
			log.Fatalf("初始化模板失败: %v", err)
		}
		fmt.Println("请编辑 accounts.csv 填入你的所有账号，并在 config.json 中配置要启用的网卡接口。")
		return
	}

	// 2. 命令行手动单次模式
	if *flagManual {
		runManualMode()
		return
	}

	// 3. 读取配置文件
	cfg, err := LoadConfig(*flagConfig)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 4. 读取账号数据 (优先 CSV 格式，其次 config.json accounts 数组或 .env)
	csvPath := cfg.CSVFile
	if *flagCSV != "" {
		csvPath = *flagCSV
	}

	var accounts []*Account
	var csvHeader []string
	if csvPath != "" {
		if _, err := os.Stat(csvPath); err == nil {
			accs, hdr, err := LoadAccountsFromCSV(csvPath, cfg.PrimaryAccount)
			if err != nil {
				log.Fatalf("从CSV文件 %s 加载账号失败: %v", csvPath, err)
			}
			accounts = accs
			csvHeader = hdr
			log.Printf("成功从 %s 加载 %d 个账号，指定主账号: [%s]",
				csvPath, len(accounts), MaskString(cfg.PrimaryAccount, 3, 2))
		}
	}

	// 回退：如果无 CSV，使用 cfg.Accounts
	if len(accounts) == 0 {
		for _, acc := range cfg.Accounts {
			if acc.IsEnabled() {
				isPrim := (acc.Username == cfg.PrimaryAccount) || len(accounts) == 0
				accounts = append(accounts, &Account{
					Username:  acc.Username,
					Password:  acc.Password,
					IsPrimary: isPrim,
					Status:    AccountStatusIdle,
				})
			}
		}
	}

	if len(accounts) == 0 {
		log.Fatalf("错误: 未找到任何可用账号！请在 accounts.csv 或 config.json 中配置账号。")
	}

	// 5. 初始化账号池与跨端口协调器
	pool := NewAccountPool(csvPath, csvHeader, accounts)
	coord := NewCoordinator(cfg.CheckInterval)

	activeInterfaces := make([]InterfaceConfig, 0)
	for _, iface := range cfg.Interfaces {
		if iface.IsEnabled() {
			if iface.Interface == cfg.PrimaryInterface {
				iface.IsPrimary = true
			}
			activeInterfaces = append(activeInterfaces, iface)
		}
	}

	if len(activeInterfaces) == 0 {
		log.Fatalf("错误: 没有启用任何监控接口！请在 config.json 的 interfaces 数组中启用至少一个网卡。")
	}

	log.Printf("================ 校园网多拨智能认证守护程序 ================")
	log.Printf("已就绪网卡: %d 个 (主端口: %s, 流量优先: %v)",
		len(activeInterfaces), cfg.PrimaryInterface, cfg.SetTrafficPriority)
	log.Printf("账号池规模: [%s]", pool.GetStats())
	log.Printf("检测周期: %d 秒，重试上限: %d 次", cfg.CheckInterval, cfg.RetryMaxCount)

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
	for _, iface := range activeInterfaces {
		wg.Add(1)
		ifaceCfg := iface
		go func() {
			defer wg.Done()
			RunPortWorker(ctx, ifaceCfg, pool, coord, cfg, *flagOnce)
		}()
	}

	wg.Wait()
	log.Printf("所有接口认证任务已退出。")
}
