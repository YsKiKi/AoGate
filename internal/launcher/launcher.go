package launcher

import (
	"bufio"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func Run(args []string) {
	fs := flag.NewFlagSet("launcher", flag.ExitOnError)
	serverAddrFlag := fs.String("s", "", "Server address (overrides config.ini)")
	keyFile := fs.String("k", "keys/private.key", "Path to private key file")

	fs.Usage = func() {
		fmt.Println("用法：launcher [选项]")
		fmt.Println("选项：")
		fs.PrintDefaults()
	}

	fs.Parse(args)

	// 1. 确定服务器地址
	// 优先级: 命令行参数 > config.ini > 报错
	targetAddr := *serverAddrFlag
	cfg, err := loadConfig("config.ini")

	if targetAddr == "" {
		if err == nil {
			if v, ok := cfg["server_addr"]; ok && v != "" {
				targetAddr = v
			}
		}
	}

	// 代理配置
	enableProxy := false
	localPort := "9999"
	gameProcess := "" // 仅用于启动，不用于强制过滤

	if err == nil {
		if v, ok := cfg["enable_proxy"]; ok && strings.ToLower(v) == "true" {
			enableProxy = true
		}
		if v, ok := cfg["local_proxy_port"]; ok && v != "" {
			localPort = v
		}
		if v, ok := cfg["game_process"]; ok && v != "" {
			gameProcess = v
		}
	}

	if targetAddr == "" {
		// 如果配置文件不存在，自动创建
		if os.IsNotExist(err) {
			createDefaultConfig("config.ini")
			fmt.Println("错误：已创建配置文件 'config.ini'。")
			fmt.Println("请编辑 'config.ini' 并设置 'server_addr'，然后再次运行。")
		} else {
			fmt.Println("错误：未指定服务器地址。")
			fmt.Println("请使用 -s 参数或检查 config.ini")
		}
		pauseAndExit(1)
	}

	// 加载私钥
	keyBytes, err := os.ReadFile(*keyFile)
	if err != nil {
		log.Fatalf("读取私钥失败: %v", err)
	}

	signer, err := parsePrivateKey(keyBytes)
	if err != nil {
		log.Fatalf("解析私钥失败: %v", err)
	}

	conn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Fatalf("连接服务器失败: %v", err)
	}
	defer conn.Close()

	// 第一步：发送认证握手请求，获取 challenge
	fmt.Printf("正在向网关发送验证请求...\n")
	_, err = conn.Write([]byte("GATE_AUTH:"))
	if err != nil {
		log.Fatalf("发送握手请求失败: %v", err)
	}

	// 读取 challenge 响应
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		log.Fatalf("读取 challenge 失败: %v", err)
	}
	challengeResp := string(buf[:n])

	if !strings.HasPrefix(challengeResp, "CHALLENGE:") {
		log.Fatalf("服务器返回非预期响应: %s", challengeResp)
	}
	challengeHex := strings.TrimPrefix(challengeResp, "CHALLENGE:")

	// 第二步：签名 challenge + timestamp 并通过新连接发送
	conn.Close()

	conn2, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Fatalf("重新连接服务器失败: %v", err)
	}
	defer conn2.Close()

	ts := time.Now().Unix()
	tsStr := fmt.Sprintf("%d", ts)

	// 签名内容 = challengeHex + ":" + timestamp
	signContent := challengeHex + ":" + tsStr
	sig, err := signMessage(signer, signContent)
	if err != nil {
		log.Fatalf("签名失败: %v", err)
	}
	sigHex := hex.EncodeToString(sig)

	payload := fmt.Sprintf("GATE_RESP:%s:%s:%s", challengeHex, tsStr, sigHex)

	_, err = conn2.Write([]byte(payload))
	if err != nil {
		log.Fatalf("发送验证载荷失败: %v", err)
	}

	// 等待响应
	n, err = conn2.Read(buf)
	if err != nil && err != io.EOF {
		log.Fatalf("读取服务器响应错误: %v", err)
	}

	resp := string(buf[:n])
	if resp == "OK" {
		fmt.Println("========================================")
		fmt.Println("   验证成功！ (ACCESS GRANTED)   ")
		fmt.Println("   您的 IP 已添加至白名单。")
		fmt.Println("========================================")

		if enableProxy {
			fmt.Printf("\n[代理模式已启用]\n")
			fmt.Printf("本地监听端口: %s (IPv4)\n", localPort)
			fmt.Printf("目标服务器:   %s (IPv6/v4)\n", targetAddr)
			fmt.Println("请在游戏中连接: 127.0.0.1:" + localPort)

			// 安全说明：代理仅监听 127.0.0.1，但同机其他进程均可不经验证直接连接
			// 本功能设计用于个人开发环境，不适用于多用户共享机器
			log.Printf("[代理] 注意：同机进程可不经验证访问此代理端口，请在受信任环境中使用。")
			proxyStarted := make(chan error, 1)
			go startProxy(localPort, targetAddr, proxyStarted)
			if err := <-proxyStarted; err != nil {
				log.Fatalf("代理服务启动失败: %v", err)
			}

			if gameProcess != "" {
				fmt.Printf("正在启动游戏: %s ...\n", gameProcess)
				launchGame(gameProcess)
			}

			fmt.Println("\n正在运行转发服务，请勿关闭此窗口...")
			select {} // 永久阻塞，保持程序运行
		}

		fmt.Println("")
		if gameProcess != "" {
			fmt.Println("")
			fmt.Printf("需要启动目标应用/游戏吗？\n")
			fmt.Printf("输入 [s] 并回车：启动 %s\n", gameProcess)
			fmt.Println("直接回车：退出本程序")
			fmt.Print("> ")

			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				text := strings.TrimSpace(scanner.Text())
				if strings.EqualFold(text, "s") {
					fmt.Printf("正在启动: %s ...\n", gameProcess)
					launchGame(gameProcess)
					time.Sleep(2 * time.Second)
				}
			}
		}
	} else {
		fmt.Println("========================================")
		fmt.Println("   验证失败！ (ACCESS DENIED)    ")
		fmt.Printf("   原因: %s\n", resp)
		fmt.Println("========================================")
		os.Exit(1)
	}
}

func parsePrivateKey(keyBytes []byte) (crypto.Signer, error) {
	// 尝试 PEM 解析
	block, _ := pem.Decode(keyBytes)
	if block != nil {
		// PKCS1 (RSA)
		if block.Type == "RSA PRIVATE KEY" {
			return x509.ParsePKCS1PrivateKey(block.Bytes)
		}
		// EC Private Key
		if block.Type == "EC PRIVATE KEY" {
			return x509.ParseECPrivateKey(block.Bytes)
		}
		// PKCS8 (Universal)
		if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			if signer, ok := key.(crypto.Signer); ok {
				return signer, nil
			}
			return nil, fmt.Errorf("key is not a signer")
		}
	}

	// 尝试 Hex/Ed25519 (Legacy)
	keyStr := strings.TrimSpace(string(keyBytes))
	decodedKey, err := hex.DecodeString(keyStr)
	if err == nil {
		if len(decodedKey) == ed25519.PrivateKeySize {
			return ed25519.PrivateKey(decodedKey), nil
		}
	}
	// 如果没有十六进制编码，也可以尝试使用原始字节作为 Ed25519 私钥
	if len(keyBytes) == ed25519.PrivateKeySize {
		return ed25519.PrivateKey(keyBytes), nil
	}

	return nil, fmt.Errorf("unknown key format")
}

func signMessage(signer crypto.Signer, message string) ([]byte, error) {
	msgBytes := []byte(message)

	// Ed25519
	if _, ok := signer.(ed25519.PrivateKey); ok {
		return signer.Sign(rand.Reader, msgBytes, crypto.Hash(0))
	}

	// RSA / ECDSA -> Hash first
	hashed := sha256.Sum256(msgBytes)
	return signer.Sign(rand.Reader, hashed[:], crypto.SHA256)
}

// launchGame 启动游戏，target 支持以下格式：
//   - Steam App ID（纯数字，如 105600）
//   - steam:// 协议 URL（如 steam://run/105600 泰拉瑞亚）
//   - 可执行文件路径（如 C:\Games\game.exe 或 /usr/bin/game）
func launchGame(target string) {
	var cmd *exec.Cmd

	launch := target

	// 纯数字视为 Steam App ID
	if _, err := strconv.ParseUint(launch, 10, 64); err == nil {
		launch = "steam://run/" + launch
	}

	isSteam := strings.HasPrefix(launch, "steam://")

	switch runtime.GOOS {
	case "windows":
		if isSteam {
			cmd = exec.Command("cmd", "/c", "start", "", launch)
		} else {
			cmd = exec.Command("cmd", "/C", "start", "", launch)
		}
	case "darwin":
		cmd = exec.Command("open", launch)
	case "linux":
		if isSteam {
			cmd = exec.Command("xdg-open", launch)
		} else {
			cmd = exec.Command(launch)
		}
	default:
		fmt.Println("当前系统不支持自动启动游戏。")
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Printf("启动游戏失败: %v\n", err)
	}
}

func loadConfig(path string) (map[string]string, error) {
	config := make(map[string]string)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			config[key] = val
		}
	}
	return config, scanner.Err()
}

func createDefaultConfig(path string) {
	content := `# Launcher Config
# Server Address (IP:Port)
server_addr=

# Proxy Mode (IPv4 to IPv6 Bridge) / 代理模式
# Set to true if the game only supports IPv4 but server is IPv6
# 开启后，Launcher 会在本地监听端口并转发流量到服务器
enable_proxy=false
local_proxy_port=9999
# Optional: Game to launch after auth / 可选：验证成功后自动启动的游戏
# Supported formats / 支持格式:
#   Steam App ID  : 105600
#   Steam URL     : steam://run/105600
#   Executable    : C:\Games\Terraria\Terraria.exe
game_process=
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Printf("警告：创建配置文件 %s 失败: %v\n", path, err)
	}
}

func startProxy(localPort, remoteAddr string, startErr chan<- error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:"+localPort)
	if err != nil {
		startErr <- err
		return
	}
	startErr <- nil // 端口监听成功
	defer ln.Close()

	for {
		client, err := ln.Accept()
		if err != nil {
			log.Printf("代理接受连接失败: %v", err)
			continue
		}

		go func(src net.Conn) {
			defer src.Close()
			// 连接目标服务器
			dst, err := net.Dial("tcp", remoteAddr)
			if err != nil {
				log.Printf("代理无法连接目标服务器: %v", err)
				return
			}
			defer dst.Close()

			// 双向转发
			go io.Copy(dst, src)
			io.Copy(src, dst)
		}(client)
	}
}

func pauseAndExit(code int) {
	fmt.Print("按回车键退出...")
	bufio.NewReader(os.Stdin).ReadString('\n')
	os.Exit(code)
}
