# AoGate - Generic Gatekeeper

AoGate 是一个通用的 TCP 服务安全外壳（代理认证网关）。它通过密钥对（支持 Ed25519, RSA, ECDSA）进行身份验证。
只有持有正确私钥并通过 Launcher 验证的客户端，其 IP 地址才会被允许访问后端目标服务器。

## 功能特性

*   **安全防护**：保护 TCP 服务（如游戏服务器、数据库等），防止未经授权的访问。
*   **多密钥支持**：支持 Ed25519 (默认), RSA (2048), ECDSA (P-256) 算法。
*   **组件分离**：包含 Server, Launcher, Keygen, Packager 四个独立组件，各司其职。
*   **配置灵活**：支持命令行参数覆盖配置文件，支持自动生成默认配置。
*   **便捷分发**：内置打包工具，可一键生成包含客户端和私钥的 ZIP 包。

## 编译指南

请确保已安装 [Go 1.22+](https://go.dev/dl/)。

### 1. 编译 Windows 版

运行 `build.bat`。
生成文件将在 `build/windows/` 目录下：
*   `server.exe`: 服务端
*   `launcher.exe`: 客户端启动器
*   `keygen.exe`: 密钥生成工具
*   `packager.exe`: 打包分发工具

### 2. 编译 Linux 版

运行 `build_linux.bat`（在 Windows 上交叉编译）或在 Linux 下直接运行 `bash build_linux.bat` (需转换格式) 或手动编译。

生成文件将在 `build/linux/` 目录下：
*   `server_linux`: 服务端
*   `launcher_linux`: 客户端启动器
*   `keygen_linux`: 密钥生成工具
*   `packager_linux`: 打包分发工具

---

## 使用说明

### 1. 密钥生成 (Keygen)

用于生成公私钥对。

**命令**: `keygen [选项] [用户名]`

**参数**:
*   `-t <类型>`: 指定密钥类型。可选: `ed25519` (默认), `rsa`, `ecdsa`。
*   `-h`: 查看帮助。

**示例**:
```bash
# 生成默认 Ed25519 密钥 (keys/user.pub, keys/user.key)
keygen

# 指定用户名和算法
keygen -t rsa myplayer
keygen -t ecdsa admin
```

### 2. 服务端 (Server)

运行在服务器上，监听公开端口并保护本地服务。

**命令**: `server [选项]`

**参数**:
*   `-l <地址>`: 监听地址，客户端连接此端口 (如 `:9999`)。
*   `-b <地址>`: 后端真实服务地址 (如 `127.0.0.1:8080`)。
*   `-k <目录>`: 公钥文件所在目录 (默认 `keys`)。
*   `-c <文件>`: 配置文件路径 (默认 `config.yaml`)。
*   `-h`: 查看帮助。

**配置**:
首次运行会自动生成 `config.yaml`，包含详细注释。

**部署示例**:
1.  将 `server` 和 `keys/` 目录上传到服务器。
2.  确保 keys 目录下有 `keygen` 生成的 `.pub` 公钥文件。
3.  启动服务：
    ```bash
    ./server -l :9999 -b 127.0.0.1:8080
    ```
    (建议配合 `nohup` 或 Systemd 使用)

### 3. 打包分发 (Packager)

用于开发/管理人员将客户端工具与私钥打包，方便发送给用户。
**前提**: 确保 `launcher.exe` 在同一目录下（或 build 目录中）。

**命令**: `packager` (按照交互提示操作)

### 4. 客户端 (Launcher)

分发给最终用户。用户只需双击运行或生成快捷方式。

**配置文件**: `config.ini` (会自动生成，需填写 server_addr)
**私钥**: 放在 `keys/private.key` (Packager 会自动处理好)

**命令**: `aogate packager`

**交互流程**:
1.  程序会自动检测目录下的 `aogate.exe` 或 `launcher.exe`。
2.  扫描 `keys/` 目录下的私钥 (`.key`)。
3.  用户选择要打包的密钥（支持批量）。
4.  输入网关服务器地址（如 `1.2.3.4:9999`）。
5.  生成的 `.zip` 包即包含配置好的客户端。

*注意：如果打包的是统一版 `aogate.exe`，压缩包内会附带 `start_launcher.bat` 脚本，用户双击脚本即可启动 Launcher 模式。*

### 4. 客户端启动器 (Launcher)

用户端运行，用于进行身份验证。

**命令**: `aogate launcher [选项]`

**参数**:
*   `-s <地址>`: 服务器地址 (如 `1.2.3.4:9999`)。优先于配置文件。
*   `-k <文件>`: 私钥文件路径 (默认 `keys/private.key`)。
*   `-h`: 查看帮助。

**用户使用流程**:
1.  解压管理员发送的压缩包。
2.   (如果是统一版) 双击 `start_launcher.bat`。
     (如果是独立版) 双击 `launcher.exe`。
3.  如果是首次运行且未配置 IP，程序会生成 `config.ini` 并提示修改。
4.  验证成功后显示 **ACCESS GRANTED**。
5.  此时 IP 已在白名单中，用户可直接打开业务客户端（如游戏客户端）连接服务器端口。

---

## 配置文件详解

### Server (`config.yaml`)
支持热重载（部分参数）或重启生效。
```yaml
listen_addr: ":9999"        # 对外监听端口
backend_addr: "127.0.0.1:8080" # 本地受保护服务
key_path: "keys"            # 公钥目录
auth_validity: 12h0m0s      # 白名单有效期
log_dir: "log"              # 日志目录
monitor_addr: ""            # 监控上报地址(可选)
```

### Launcher (`config.ini`)
简单的键值对配置。
```ini
# Server Address
server_addr=1.2.3.4:9999
```

## 目录结构说明

```text
AoGate/
├── cmd/                # 各组件入口
│   └── aogate/         # 统一入口
├── internal/           # 核心逻辑模块
│   ├── server/         # 网关服务逻辑
│   ├── launcher/       # 启动器逻辑
│   ├── keygen/         # 密钥生成逻辑
│   └── packager/       # 打包工具逻辑
├── build/              # 编译输出目录
├── keys/               # 存放生成的密钥对
└── README.md           # 说明文档
```

## 安全建议

1.  **防火墙设置**: 务必配置服务器防火墙（如 security group 或 iptables），**禁止外网直接访问后端真实端口**（如 8080），只允许 `127.0.0.1` 访问。
2.  **私钥保护**: 私钥 (`.key`) 等同于密码，请务必妥善保管，不要通过不安全的渠道传输。
3.  **定期轮换**: 建议定期重新生成密钥并分发给长期用户。
