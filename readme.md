# SCBT 校园网自动认证程序 (支持单拨/多拨/大账号池轮询)

针对高校/园区 CMCC Portal 认证系统的自动登录与多拨守护程序。支持单账号常驻、单线多拨（Macvlan / MWAN3）多接口并发认证、海量账号 CSV 账号池自动轮换、智能跨端口断线判定，以及主账号流量优先级控制。

---

## ✨ 核心特性

- **海量账号 CSV 账号池**：支持导入包含数十上百个账号的 `accounts.csv` 文件，自动管理账号生命周期（空闲待用、使用中、异常冷却、自动跳过已过期账号）。
- **智能跨端口存活检测与故障切换 (Failover)**：
  - **单端口掉线识别**：当某个端口掉线时，自动检测其他端口状态。若其他端口正常在线，说明校园网整体畅通，判定为当前账号被踢下线、欠费或到期，**立即自动切换到下一个未使用的账号**登录，保障网口不中断。
  - **全局断网保护**：若所有端口均掉线，程序智能判定为校园网整体断网（夜间断网/机房维护/网线松脱），**绝不盲目轮换耗尽账号池**，保持当前账号静候网络恢复。
- **主账号指定与流量优先级保证**：
  - 配置文件中可指定唯一 `primary_account` 与 `primary_interface`（如 `eth1`）。
  - 主账号专属绑定到主网卡，开启 `set_traffic_priority` 后自动在 Linux/OpenWrt 内核中维持最低路由跃点（`metric 10`），确保局域网所有设备的日常网络与游戏流量优先走主账号端口。
- **严格网卡物理/逻辑绑定**：采用 Linux `SO_BINDTODEVICE` 与源 IP 双重绑定机制，确保认证和连通性检测报文 100% 严格走指定物理/虚拟网口，不受 MWAN3 策略路由干扰。
- **多种运行模式**：
  - **守护模式**：多端口后台常驻监控与自动重连。
  - **单次模式 (`-once`)**：执行一次全网卡检测/登录后即刻退出，适合集成到 hotplug / crontab。
  - **纯手动模式 (`-manual`)**：直接在命令行指定接口和学号密码登录，无需配置文件。

---

## 📋 配置文件与账号格式

### 1. 账号池文件 (`accounts.csv`)

支持标准的 CSV 格式，首行包含表头即可自动识别字段：

```csv
"学号","密码","套餐","过期时间"
"255102010828","094217","100M包学期2终端-学生","2030-10-31 00:00:00"
"255602130329","257607","100M包学期2终端-学生","2030-10-31 00:00:00"
"255801020304","123456","100M包学期2终端-学生","2030-10-31 00:00:00"
```

> **说明**：
> - 表头支持中文（`学号`、`密码`、`套餐`、`过期时间`）或英文（`username`、`password` 等）。
> - 若账号已过“过期时间”，程序会在启动时自动识别并跳过，防止无效认证。

### 2. 主配置文件 (`config.json`)

```json
{
  "csv_file": "accounts.csv",
  "primary_account": "255102010828",
  "primary_interface": "eth1",
  "set_traffic_priority": true,
  "check_interval": 15,
  "retry_maxcount": 3,
  "test_url": "https://www.baidu.com/",
  "login_url": "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/cmcc_login/",
  "success_url": "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/success/",
  "fail_url": "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/fail/",
  "default_basip": "10.241.0.11",
  "default_nasid": "4",
  "interfaces": [
    {
      "name": "主WAN",
      "interface": "eth1",
      "is_primary": true,
      "enabled": true
    },
    {
      "name": "副WAN1",
      "interface": "vwan1",
      "is_primary": false,
      "enabled": true
    },
    {
      "name": "副WAN2",
      "interface": "vwan2",
      "is_primary": false,
      "enabled": false
    }
  ]
}
```

> **字段说明**：
> - `csv_file`: 存放账号的 CSV 文件路径。
> - `primary_account`: 主账号学号。如果留空，默认使用 CSV 中第 1 个有效账号。
> - `primary_interface`: 主网卡名称（通常为物理 WAN 口 `eth1`）。
> - `set_traffic_priority`: 是否自动将主网卡默认路由 metric 设为 10，让路由器优先走主端口。
> - `interfaces`: 要监听的多拨接口列表。`mac` 和 `ip` 留空即可，会自动从网卡读取。

---

## 🚀 常用运行命令

```bash
# 一键生成 accounts.csv 与 config.json 模板
./scbt-network-autologin -init

# 正常前台运行（监控所有已启用的接口）
./scbt-network-autologin

# 指定配置文件或 CSV 文件
./scbt-network-autologin -c /root/a/config.json -csv /root/a/accounts.csv

# 单次执行模式 (检测/登录后立即退出)
./scbt-network-autologin -once

# 纯手动模式 (直接命令行单跑指定接口)
./scbt-network-autologin -manual -i vwan1 -u 255102010828 -p 094217
```

---

## 🛠️ OpenWrt 服务化管理

在 OpenWrt 路由器上，可通过内置的 procd 服务管理：

```bash
service scbt-autologin start    # 启动后台守护
service scbt-autologin stop     # 停止服务
service scbt-autologin restart  # 重启服务
service scbt-autologin enable   # 设置开机自启
logread -f | grep scbt          # 查看实时认证和切号日志
```
