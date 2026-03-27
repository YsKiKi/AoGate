# Changelog

## [1.0.0] - 2026-03

### 🎉 首次发布


AoGate 是一个通用的 TCP 服务安全外壳（代理认证网关），通过密钥对进行身份验证，保护后端服务免受未授权访问。

这个项目最初是为 Terraria / Minecraft 自建服务器而创建：当原服务的白名单或登录机制无法完全阻止陌生人闯入和破坏时，通过额外的网关验证，为朋友联机提供更可靠的访问控制。

### ✨ 新增功能

#### 核心组件
- **Server** - 网关服务端
  - TCP 代理与认证网关
  - 支持 Ed25519、RSA、ECDSA 公钥验证
  - IP 白名单管理（支持有效期配置）
  - 自动日志轮转（按大小和数量限制）
  - WebSocket 监控事件上报
  - YAML 配置文件（首次运行自动生成）

- **Launcher** - 客户端启动器
  - 私钥签名认证
  - INI 配置文件支持
  - 可选本地代理模式
  - 支持启动外部应用程序

- **Keygen** - 密钥生成工具
  - 支持 Ed25519（默认）、RSA-2048、ECDSA-P256 算法
  - 自定义用户名

- **Packager** - 打包分发工具
  - 交互式密钥选择
  - 自动打包 Launcher + 私钥 + 配置
  - 支持 Windows 和 Linux 平台打包
  - 生成即用型 ZIP 分发包

#### 配置与管理
- 服务端 `config.yaml` 支持热重载
- 客户端 `config.ini` 简洁配置
- 白名单自动持久化

#### 安全特性
- 多种加密算法支持
- 挑战-响应认证机制
- 签名验证防重放攻击

### 📦 构建支持
- Windows 原生编译 (`build.bat`)
- Linux 交叉编译 (`build_linux.bat`)

---

[1.0.0]: https://github.com/YsKiKi/AoGate/releases/tag/v1.0.0


## [1.0.3] - 2026-03-27

### ✨ 新增功能

#### 速率限制与 IP 封禁
- **连接频率限制**：同一 IP 在可配置时间窗口 `t` 内超过 `n` 次连接尝试后，自动封禁 `m` 时长
  - 三项参数均可在 `config.yaml` 中配置：`rate_limit_window`（默认 `1m`）、`rate_limit_max`（默认 `30`）、`ban_duration`（默认 `10m`）
  - 将 `rate_limit_max` 设为 `0` 可完全禁用此功能
- **IPv6 /64 前缀聚合**：对 IPv6 地址以高 64 位（/64 网段）作为限速键，防止攻击者通过轮换低 64 位绕过封禁
- **封禁持久化**：触发封禁时立即写入 `banned_ip.txt`，服务重启后自动恢复有效封禁记录，过期条目在 cleanupLoop 周期内自动清理
  - 文件格式：`<key> <unix_expiry>`，支持手动删除行或将过期时间设为 `0` 以解封（重启后生效）
- 封禁事件同步写入 `blocked.log` 并上报监控 WebSocket

---

[1.0.0]: https://github.com/YsKiKi/AoGate/releases/tag/v1.0.0
[1.0.3]: https://github.com/YsKiKi/AoGate/releases/tag/v1.0.3
