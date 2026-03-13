# AoGate 使用手册

AoGate 是一个通用 TCP 服务安全网关，通过非对称密钥对进行身份验证。只有持有合法私钥并通过 Launcher 认证的客户端，其 IP 才会被加入白名单，进而访问被保护的后端服务。

---

## 目录

- [keygen — 密钥生成工具](#keygen--密钥生成工具)
- [server — 网关服务端](#server--网关服务端)
- [packager — 打包分发工具](#packager--打包分发工具)
- [launcher — 客户端启动器](#launcher--客户端启动器)
- [整体工作流程](#整体工作流程)
- [WebSocket 监控上报](#websocket-监控上报)
- [安全说明](#安全说明)

---

## keygen — 密钥生成工具

为用户生成非对称密钥对（公钥 + 私钥）。

### 命令格式

```
keygen [-t <算法>] [用户名]
```

### 参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-t <算法>` | 密钥算法：`ed25519` / `rsa` / `ecdsa` | `ed25519` |
| `[用户名]` | 生成的密钥文件名前缀 | `user` |

### 输出

在 `keys/` 目录下生成两个文件：
- `keys/<用户名>.pub` — 公钥（部署到服务端）
- `keys/<用户名>.key` — 私钥（通过 packager 分发给用户）

### 示例

```bash
# 生成默认 Ed25519 密钥：keys/user.pub + keys/user.key
keygen

# 为玩家 alice 生成 RSA 密钥
keygen -t rsa alice

# 为管理员生成 ECDSA 密钥
keygen -t ecdsa admin
```

### 支持的算法

| 算法 | 私钥格式 | 公钥格式 | 说明 |
|------|----------|----------|------|
| `ed25519` | 64字节 Hex 文本 | 32字节 Hex 文本 | 默认推荐，速度最快 |
| `rsa` | PEM (PKCS#1) | PEM (PKIX) | RSA 2048位 |
| `ecdsa` | PEM (EC) | PEM (PKIX) | ECDSA P-256 |

---

## server — 网关服务端

运行在服务器上，监听公开端口，保护本地后端服务。

### 命令格式

```
server [-l <地址>] [-b <地址>] [-k <目录>] [-c <文件>]
```

### 参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-l <地址>` | 公开监听地址，客户端连接此端口（如 `:9999`） | 读配置文件 |
| `-b <地址>` | 后端真实服务地址（如 `127.0.0.1:8080`） | 读配置文件 |
| `-k <目录>` | 公钥文件所在目录（只加载 `.pub` 文件） | `keys` |
| `-c <文件>` | 配置文件路径 | `config.yaml` |

**命令行参数优先级高于配置文件。**  
首次运行且无配置文件时，自动生成带注释的 `config.yaml`。

### 配置文件 (`config.yaml`)

```yaml
# 公开监听地址（客户端连接此端口）
listen_addr: ":9999"

# 后端真实服务地址（被保护的本地服务）
backend_addr: "127.0.0.1:8080"

# 公钥目录（只读取 .pub 文件）
key_path: "keys"

# WebSocket 监控上报地址（留空则完全禁用监控功能）
monitor_addr: ""

# 认证后 IP 白名单有效期（支持 Go duration 格式：12h、30m、1h30m）
auth_validity: "12h"

# 日志目录
log_dir: "log"

# 单个日志文件最大字节数（轮换阈值）
max_log_size: 10485760

# 保留的日志文件数量（超出时删除最旧的）
max_log_files: 3
```

### 功能说明

- **IP 白名单机制**：客户端通过 Launcher 验证后，其 IP 被加入白名单，有效期内（默认 12 小时）所有 TCP 连接直接放行至后端
- **IP 绑定**：同一 IP 在白名单有效期内只能绑定一个用户密钥，防止 IP 共享滥用
- **重放攻击防护**：每个认证签名（nonce）最多使用一次，4 分钟内同一签名重复提交将被拒绝并返回 `REPLAY`
- **时间戳校验**：认证包时间戳需在服务器当前时间 ±2 分钟内，防止长期重放
- **IP 规范化**：自动将 IPv4-mapped IPv6 地址（`::ffff:x.x.x.x`）统一为 IPv4 格式，避免同一 IP 被识别为不同地址
- **多公钥支持**：`keys/` 目录下所有 `.pub` 文件均被加载，支持多用户同时接入
- **白名单持久化**：白名单保存至 `whitelist.json`，服务重启后已认证会话仍然有效
- **日志自动轮换**：主日志自动按大小轮换，保留指定数量的历史文件；拦截日志单独记录至 `log/blocked.log`
- **WebSocket 监控**：可选上报连接事件至外部 WebSocket 服务；`monitor_addr` 留空则完全禁用，无任何后台连接尝试
- **TCP 优化**：对白名单连接的转发启用 `TCP_NODELAY` 和 `KeepAlive`

### 认证响应码

| 响应 | 含义 |
|------|------|
| `OK` | 认证成功，IP 已加入白名单 |
| `EXPIRED` | 认证包时间戳超过 ±2 分钟 |
| `REPLAY` | 该签名已被使用（重放攻击） |
| `LOCKED` | 此 IP 已被其他用户绑定 |
| `FAIL` | 签名校验失败（密钥不匹配） |

### 示例

```bash
# 使用命令行参数启动
./server -l :9999 -b 127.0.0.1:25565

# 使用配置文件启动（默认读取 config.yaml）
./server

# 指定自定义配置文件
./server -c /etc/aogate/config.yaml

# 首次运行（无配置文件时自动生成模板）
./server
```

### 部署建议

1. 将 `server`（或 `server_linux`）和 `keys/` 目录上传至服务器
2. 确保 `keys/` 目录中存在 `keygen` 生成的 `.pub` 公钥文件
3. 配合 `systemd` 或 `nohup` 保持后台运行：
   ```bash
   nohup ./server_linux -l :9999 -b 127.0.0.1:8080 > /dev/null 2>&1 &
   ```

---

## packager — 打包分发工具

将编译好的 Launcher 可执行文件与指定用户的私钥打包为 ZIP，方便发送给对应用户。

### 命令格式

```
packager [-platform <平台>] [-h]
```

### 参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-platform <平台>` | 目标平台（见下表）；留空时交互式选择 | 交互选择 |
| `-h` / `-help` | 显示帮助信息 | — |

### 支持的平台

| `-platform` 值 | 说明 |
|----------------|------|
| `windows-amd64` | Windows x64（默认搜索 `build/windows/`） |
| `windows-arm64` | Windows ARM64（搜索 `build/windows/arm64/`） |
| `linux-amd64` | Linux x64（搜索 `build/linux/`） |
| `linux-arm64` | Linux ARM64（搜索 `build/linux/arm64/`） |
| `darwin-amd64` | macOS x64（搜索 `build/darwin/`） |
| `darwin-arm64` | macOS Apple Silicon（搜索 `build/darwin/arm64/`） |

### 交互流程

1. **选择目标平台**（命令行指定则跳过）
2. **在指定目录自动搜索** Launcher 可执行文件
3. **列出** `keys/` 目录下所有 `.key` 私钥文件
4. **选择要打包的密钥**（单选、多选 `1,3`、全选 `0`）
5. **输入服务器地址**（写入 ZIP 内的 `config.ini`）
6. **为每个选中的密钥生成** `Launcher_<用户名>_<平台>.zip`

### ZIP 包内容

| 文件 | 说明 |
|------|------|
| `launcher.exe` / `launcher` | 客户端可执行文件 |
| `keys/private.key` | 用户私钥（已重命名） |
| `config.ini` | 已预填 `server_addr` 的配置文件 |
| `请勿将这些内容分享给他人，您可能因此受到影响` | 安全提示占位文件 |

### 示例

```bash
# 交互式运行（逐步选择平台和密钥）
packager

# 直接指定目标平台
packager -platform linux-amd64

# 批量打包 Windows x64 版本
packager -platform windows-amd64
```

---

## launcher — 客户端启动器

最终用户使用的工具。向网关发送签名验证请求，将自身 IP 加入白名单。

### 命令格式

```
launcher [-s <服务器地址>] [-k <私钥路径>]
```

### 参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-s <地址>` | 服务器地址（覆盖 `config.ini` 中的 `server_addr`） | 读配置文件 |
| `-k <路径>` | 私钥文件路径 | `keys/private.key` |

**优先级：命令行 `-s` > `config.ini` 中的 `server_addr`**

### 配置文件 (`config.ini`)

首次运行时若文件不存在，自动生成模板。

```ini
# 服务器地址（必填）
server_addr=1.2.3.4:9999

# 代理模式：将本地 IPv4 端口转发至服务器（适用于游戏只支持 IPv4 但服务器为 IPv6 的情况）
enable_proxy=false

# 代理模式本地监听端口
local_proxy_port=9999

# 验证成功后自动启动的程序（可选）
# 支持格式：
#   Steam App ID  : 105600
#   Steam URL     : steam://run/105600
#   可执行文件路径 : C:\Games\Terraria\Terraria.exe
game_process=
```

### 运行流程

1. 读取私钥 → 对当前时间戳生成签名
2. 发送 `GATE_AUTH:<时间戳>:<签名HEX>` 至服务器
3. 收到 `OK` → 打印成功信息
4. 若 `enable_proxy=true`：在本地启动 TCP 转发服务（`127.0.0.1:<local_proxy_port>` → 服务器），游戏连接本地端口即可
5. 若配置了 `game_process`：提示是否启动（代理模式下自动启动）

### 代理模式说明

适用场景：游戏客户端不支持 IPv6，但服务器仅有 IPv6 地址。

- 开启后 Launcher **必须保持运行**，不可关闭
- 游戏连接地址改为 `127.0.0.1:<local_proxy_port>`（默认 `127.0.0.1:9999`）
- **注意**：代理端口仅绑定 `127.0.0.1`，但同机其他进程均可不经认证直接访问，请在受信任的个人环境中使用

### `game_process` 支持的格式

| 格式 | 示例 | 说明 |
|------|------|------|
| Steam App ID | `105600` | 自动转为 `steam://run/105600` |
| Steam URL | `steam://run/105600` | 直接使用 |
| 可执行文件路径 | `C:\Games\game.exe` | Windows / Linux / macOS 均支持 |

### 示例

```bash
# 使用配置文件中的服务器地址
launcher

# 命令行临时指定服务器地址
launcher -s 1.2.3.4:9999

# 指定自定义私钥路径
launcher -k /path/to/my.key

# 同时指定服务器和私钥
launcher -s 1.2.3.4:9999 -k keys/alice.key
```

---

## 整体工作流程

```
【管理员侧】
  1. keygen alice                    → 生成 keys/alice.pub + keys/alice.key
  2. 将 keys/alice.pub 放到服务器的 keys/ 目录
  3. 启动服务端：server -l :9999 -b 127.0.0.1:25565
  4. packager -platform windows-amd64
     → 选择 alice.key，输入服务器地址
     → 生成 Launcher_alice_windows-amd64.zip

【分发】
  5. 将 Launcher_alice_windows-amd64.zip 发送给用户 alice

【用户侧】
  6. 解压 ZIP → 运行 launcher.exe
     → 签名验证通过 → alice 的 IP 加入白名单（有效期 12h）
     → 后续游戏流量直连服务器，无需再次认证
```

---

## WebSocket 监控上报

AoGate 支持通过 WebSocket 将连接事件实时上报至外部监控服务，便于实现接入日志、审计、告警等功能。

### 启用方式

在 `config.yaml` 中配置 `monitor_addr` 即可启用：

```yaml
# 填写监控服务的 WebSocket 地址
monitor_addr: "ws://127.0.0.1:8888"
```

**留空则完全禁用监控，不会建立任何后台 WebSocket 连接。**

地址格式支持以下三种写法：

| 输入格式 | 解析结果 | 说明 |
|----------|----------|------|
| `ws://1.2.3.4:8888` | `ws://1.2.3.4:8888` | 完整 URL，直接使用 |
| `1.2.3.4:8888` | `ws://1.2.3.4:8888` | 自动补充 `ws://` 前缀 |
| `8888` | `ws://127.0.0.1:8888` | 纯端口号，默认连接本机 |

### 连接机制

- 使用 Gorilla WebSocket 库连接监控服务端
- 内置**自动重连**：连接失败时每 5 秒重试，连接断开后每 3 秒重试
- 事件通过异步通道发送，缓冲区容量 100 条；通道满时丢弃新事件，**不会阻塞主服务流程**

### 消息格式

每条上报消息为 JSON 格式，结构如下：

```json
{
  "type": "decision",
  "ts": 1709817600,
  "ip": "203.0.113.42",
  "id": "玩家ID或“未知”",
  "action": "allowed | blocked",
  "reason": "具体原因"
}
```

#### 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | 事件类型，固定为 `"decision"` |
| `ts` | number | Unix 时间戳（秒） |
| `ip` | string | 客户端 IP 地址 |
| `id` | string | 玩家 ID（未认证时为 `"未知"`） |
| `action` | string | 决策结果：`"允许"` 或 `"拦截"` |
| `reason` | string | 决策原因（可能为空） |

#### 事件触发场景

| action | reason | 触发时机 |
|--------|--------|----------|
| `允许` | `白名单` | IP 已在白名单中，连接直接放行 |
| `允许` | `鉴权成功` | 客户端签名验证通过，IP 加入白名单 |
| `拦截` | `未在白名单` | IP 不在白名单中，连接被拒绝 |
| `拦截` | `鉴权过期` | 认证包时间戳超过 ±2 分钟 |
| `拦截` | `签名格式错误` | 签名内容不是合法的 Hex 编码 |
| `拦截` | `重放攻击` | 签名已被使用过（防重放） |
| `拦截` | `IP已被其他玩家绑定` | 该 IP 在有效期内已绑定其他用户 |
| `拦截` | `签名校验失败` | 加密签名与所有公钥均不匹配 |

### 监控服务端示例

任何支持 WebSocket 的服务均可接收上报消息。以下是一个简单的 Node.js 示例：

```javascript
const { WebSocketServer } = require('ws');

const wss = new WebSocketServer({ port: 8888 });

wss.on('connection', (ws) => {
  console.log('AoGate connected');
  ws.on('message', (data) => {
    const event = JSON.parse(data);
    console.log(`[${event.action}] ${event.ip} (${event.id}) - ${event.reason}`);
  });
});
```

---

## 安全说明

- **私钥文件**（`.key`）务必妥善保管，**绝对不能泄露**给他人，否则对方可使用您的身份通过认证
- 私钥文件由 packager 打包时已重命名为通用名称 `keys/private.key`，**不要将压缩包分享给其他人**
- 认证机制包含时间戳校验（±2 分钟）和 nonce 去重（4 分钟内防重放），可抵御常见的截包重放攻击
- 服务端只加载 `keys/` 目录下的 `.pub` 公钥文件，私钥文件（`.key`）即使误放入 `keys/` 目录也不会被加载
