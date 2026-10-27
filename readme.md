# SCBT 校园网自动认证程序 (支持单拨/多拨)

针对高校/园区 CMCC Portal 认证系统的自动登录与多拨守护程序。支持单账号常驻、单线多拨（Macvlan / MWAN3）多接口并发认证、断线自动重连，以及全手动配置。

---

## ✨ 核心特性

- **多拨多接口并发支持**：支持 `eth1`、`vwan1`、`vwan2` 等多个虚拟网卡同时登录不同账号，互不干扰。
- **严格网卡物理/逻辑绑定**：采用 Linux `SO_BINDTODEVICE` 与源 IP 双重绑定机制，确保认证和检测请求 100% 走指定的物理/虚拟网卡，完美适配 OpenWrt MWAN3 分流。
- **断线自动重连**：针对每个网卡独立监测网络连通性，离线秒级发现并自动重连，支持指数重试与失败暂停机制。
- **智能免密/信息自适应**：自动从网卡读取 MAC 地址与 DHCP 分配的 IPv4 地址；同时支持在配置文件中手动强制指定。
- **全手动/自动化兼备**：
  - **守护模式**：加载 `config.json` 或 `.env` 后台多线程监控。
  - **单次模式 (`-once`)**：执行一次全网卡检测与登录后即刻退出，便于集成到 hotplug / cron。
  - **手动单跑 (`-manual`)**：无需配置文件，直接通过命令行参数对指定网卡执行登录。
- **向下完全兼容**：若目录中无 `config.json` 但存在 `.env`，会自动回退为单账号老版本模式。

---

## 🚀 快速使用

### 1. 生成配置文件模板

```bash
./scbt-network-autologin -init
```
执行后会在当前目录生成 `config.json` 模板文件。

### 2. 编辑配置文件 (`config.json`)

```json
{
  "check_interval": 15,
  "retry_maxcount": 5,
  "test_url": "https://www.baidu.com/",
  "login_url": "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/cmcc_login/",
  "success_url": "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/success/",
  "fail_url": "http://47.106.209.12:9770/lfradius/libs/portal/unify/portal.php/login/fail/",
  "default_basip": "10.241.0.11",
  "default_nasid": "4",
  "accounts": [
    {
      "name": "主WAN",
      "interface": "eth1",
      "username": "13000000001",
      "password": "your_password_1",
      "mac": "",
      "ip": "",
      "enabled": true
    },
    {
      "name": "多拨WAN2",
      "interface": "vwan1",
      "username": "13000000002",
      "password": "your_password_2",
      "mac": "",
      "ip": "",
      "enabled": true
    }
  ]
}
```

> **字段说明**：
> - `interface`: 绑定的网络接口（如物理网卡 `eth1`，或 macvlan 虚拟网卡 `vwan1`）。
> - `username` / `password`: 对应网卡要登录的校园网账号与密码。
> - `mac`: 手动指定的 MAC 地址（留空则自动从该接口网卡读取真实/虚拟 MAC）。
> - `ip`: 手动指定的 IP 地址（留空则自动读取 DHCP 获取到的 IPv4）。
> - `enabled`: 是否启用该账号监控（`true` / `false`）。

### 3. 运行程序

```bash
# 后台监控常驻运行 (默认读取 config.json)
./scbt-network-autologin

# 指定配置文件路径
./scbt-network-autologin -c /etc/scbt/config.json

# 单次检测登录模式 (常用于 OpenWrt ifup hotplug 脚本或调试)
./scbt-network-autologin -once

# 纯手动模式：直接通过命令行登录指定网卡
./scbt-network-autologin -manual -i vwan1 -u 13000000002 -p mypassword
```

---

## 🛠️ OpenWrt 单线多拨配置简明指引

1. **安装 macvlan**：
   ```bash
   opkg update && opkg install kmod-macvlan mwan3 luci-app-mwan3
   ```
2. **在 `/etc/config/network` 创建虚拟 WAN 接口**：
   ```uci
   config device
       option name 'vwan1'
       option type 'macvlan'
       option ifname 'eth1'
       option mode 'vepa'
       option macaddr '82:af:ca:b5:2a:93'

   config interface 'vwan1'
       option proto 'dhcp'
       option device 'vwan1'
   ```
3. 在 `config.json` 中配置对应的 `vwan1` 与对应账号密码。
4. 运行 `scbt-network-autologin`，即可完成全自动的多拨登录。
