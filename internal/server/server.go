package server

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// 配置常量 (常量仅保留非配置项)
const (
	AuthPrefix      = "GATE_AUTH:"
	CleanupInterval = 10 * time.Minute
	WhitelistFile   = "whitelist.json"
)

// ConfigDuration 自定义Duration以支持YAML字符串格式
type ConfigDuration time.Duration

func (d *ConfigDuration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err == nil {
		pd, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*d = ConfigDuration(pd)
		return nil
	}
	var i int64
	if err := value.Decode(&i); err == nil {
		*d = ConfigDuration(i)
		return nil
	}
	return fmt.Errorf("invalid duration")
}

func (d ConfigDuration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// ServerConfig 配置结构体
type ServerConfig struct {
	ListenAddr   string         `yaml:"listen_addr"`
	BackendAddr  string         `yaml:"backend_addr"`
	KeyPath      string         `yaml:"key_path"`
	MonitorAddr  string         `yaml:"monitor_addr"`
	AuthValidity ConfigDuration `yaml:"auth_validity"`
	LogDir       string         `yaml:"log_dir"`
	MaxLogSize   int64          `yaml:"max_log_size"`
	MaxLogFiles  int            `yaml:"max_log_files"`
}

// 默认配置
var config = ServerConfig{
	ListenAddr:   "",
	BackendAddr:  "",
	KeyPath:      "keys",
	MonitorAddr:  "",
	AuthValidity: ConfigDuration(12 * time.Hour),
	LogDir:       "log",
	MaxLogSize:   10 * 1024 * 1024, // 10MB
	MaxLogFiles:  3,
}

// Session 结构体
type Session struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NamedKey 带名称的公钥 (Changed from ed25519.PublicKey to crypto.PublicKey)
type NamedKey struct {
	ID  string
	Key crypto.PublicKey
}

// 全局状态
var (
	whitelist         = make(map[string]Session)
	pendingChallenges = make(map[string]time.Time) // challenge hex -> 过期时间，一次性使用
	mu                sync.RWMutex
	pubKeys           []NamedKey
	backend           string
	blockLogger       *log.Logger
	// buffer pool for initial handshake
	bufPool = sync.Pool{
		New: func() interface{} {
			b := make([]byte, 256)
			return b
		},
	}
)

// RotatingLogger 实现自动轮换的日志记录器
type RotatingLogger struct {
	mu       sync.Mutex
	currFile *os.File
	currSize int64
}

func (l *RotatingLogger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 如果没有文件或大小超过限制，进行轮换
	if l.currFile == nil || l.currSize+int64(len(p)) > config.MaxLogSize {
		if err := l.rotate(); err != nil {
			// 如果轮换失败，回退到标准错误输出
			os.Stderr.Write(p)
			return len(p), nil
		}
	}

	n, err = l.currFile.Write(p)
	l.currSize += int64(n)
	return n, err
}

func (l *RotatingLogger) rotate() error {
	if l.currFile != nil {
		l.currFile.Close()
	}

	// 确保目录存在
	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		return err
	}

	// 生成新的文件名 (使用纳秒级时间戳避免冲突)
	filename := filepath.Join(config.LogDir, fmt.Sprintf("server_%d.log", time.Now().UnixNano()))
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	l.currFile = f
	l.currSize = 0

	// 异步清理旧文件
	go l.cleanup()

	return nil
}

func (l *RotatingLogger) cleanup() {
	entries, err := os.ReadDir(config.LogDir)
	if err != nil {
		return
	}

	var logFiles []string
	for _, e := range entries {
		// 只检查符合模式的文件
		if !e.IsDir() && strings.HasPrefix(e.Name(), "server_") && strings.HasSuffix(e.Name(), ".log") {
			logFiles = append(logFiles, filepath.Join(config.LogDir, e.Name()))
		}
	}

	if len(logFiles) <= config.MaxLogFiles {
		return
	}

	// 排序（默认按名字排序，因为使用了时间戳，所以也是按时间排序）
	sort.Strings(logFiles)

	// 删除最旧的文件
	toRemove := len(logFiles) - config.MaxLogFiles
	for i := 0; i < toRemove; i++ {
		os.Remove(logFiles[i])
	}
}

// 初始化拦截日志记录器
func initBlockLogger() {
	if err := os.MkdirAll(config.LogDir, 0755); err != nil {
		log.Printf("Failed to create log dir: %v", err)
		return
	}
	// append 模式，不自动删除
	f, err := os.OpenFile(filepath.Join(config.LogDir, "blocked.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open blocked.log: %v", err)
		return
	}
	blockLogger = log.New(f, "", log.LstdFlags)
}

// saveConfigWithComments 保存带详细中英注释的配置文件
func saveConfigWithComments(path string) {
	content := fmt.Sprintf(`# Gateway Server Configuration / 网关服务器配置
# Generated on / 生成时间: %s

# Listen address / 监听地址
# The public port for clients to connect (e.g., :9999)
listen_addr: "%s"

# Backend address / 后端真实服务地址
# The local service to protect (e.g., 127.0.0.1:8080)
backend_addr: "%s"

# Key path / 密钥目录
# Directory containing .pub keys
key_path: "%s"

# Monitor address / 监控上报地址
# WebSocket server to report access logs (optional)
monitor_addr: "%s"

# Auth validity / 认证有效期
# How long an IP remains effective after authentication
auth_validity: %s

# Log directory / 日志目录
log_dir: "%s"

# Max log size / 单个日志最大大小 (bytes)
# Default: 10485760 (10MB)
max_log_size: %d

# Max log files / 保留日志文件数量
max_log_files: %d
`,
		time.Now().Format(time.RFC3339),
		config.ListenAddr,
		config.BackendAddr,
		config.KeyPath,
		config.MonitorAddr,
		time.Duration(config.AuthValidity).String(),
		config.LogDir,
		config.MaxLogSize,
		config.MaxLogFiles,
	)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		log.Printf("Failed to write config file %s: %v", path, err)
	}
}

// loadWhitelist 加载持久化白名单
func loadWhitelist() {
	f, err := os.Open(WhitelistFile)
	if err != nil {
		return
	}
	defer f.Close()

	var loaded map[string]Session
	if err := json.NewDecoder(f).Decode(&loaded); err == nil {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		for ip, sess := range loaded {
			// 只加载未过期的
			if sess.ExpiresAt.After(now) {
				whitelist[ip] = sess
			}
		}
		log.Printf("Loaded %d active sessions from disk", len(whitelist))
	}
}

// saveWhitelist 保存白名单到磁盘 (Caller must hold lock)
func saveWhitelist() {
	f, err := os.Create(WhitelistFile)
	if err != nil {
		log.Printf("Failed to save whitelist: %v", err)
		return
	}
	defer f.Close()

	json.NewEncoder(f).Encode(whitelist)
}

// Run is the entry point
func Run(args []string) {
	// Setup flags
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	listenAddr := fs.String("l", "", "Listen address (e.g., :9999)")
	backendAddr := fs.String("b", "", "Backend address (e.g., 127.0.0.1:8080)")
	keyPath := fs.String("k", "keys", "Path to keys directory")
	configFile := fs.String("c", "config.yaml", "Path to config file")

	fs.Usage = func() {
		fmt.Println("用法：server [选项]")
		fmt.Println("选项：")
		fs.PrintDefaults()
	}

	fs.Parse(args)

	// 1. 尝试加载配置文件
	f, err := os.Open(*configFile)
	if err == nil {
		// 如果配置文件存在，读取配置
		defer f.Close()
		decoder := yaml.NewDecoder(f)
		if err := decoder.Decode(&config); err != nil {
			log.Printf("Failed to parse config file: %v, using defaults", err)
		} else {
			log.Printf("Loaded config from %s", *configFile)
		}
	} else {
		log.Printf("Config file not found, using defaults")
	}

	// 2. 命令行参数覆盖配置文件
	if *listenAddr != "" {
		config.ListenAddr = *listenAddr
	}
	if *backendAddr != "" {
		config.BackendAddr = *backendAddr
	}
	if *keyPath != "" {
		config.KeyPath = *keyPath
	}

	// 3. 如果没配置监听或后端，且是第一次运行（无配置文件），提示或生成默认
	if config.ListenAddr == "" && config.BackendAddr == "" {
		// 都不存在，写入默认demo配置，方便用户修改
		config.ListenAddr = ":9999"
		config.BackendAddr = "127.0.0.1:8080"
		saveConfigWithComments(*configFile)
		log.Printf("Generated default config file %s, please modify it.", *configFile)
	}

	// 设置全局
	backend = config.BackendAddr

	// 设置日志
	rLogger := &RotatingLogger{}
	// 同时输出到控制台和文件
	log.SetOutput(io.MultiWriter(os.Stdout, rLogger))

	// 初始化拦截日志
	initBlockLogger()

	// 初始化监控客户端
	initMonitor(config.MonitorAddr)

	// 加载公钥 (支持文件或目录)
	if err := loadPublicKeys(config.KeyPath); err != nil {
		log.Fatalf("Failed to load keys: %v", err)
	}
	if len(pubKeys) == 0 {
		log.Fatalf("No valid public keys found in %s", config.KeyPath)
	}
	log.Printf("Loaded %d public keys", len(pubKeys))

	// 加载持久化白名单
	loadWhitelist()

	// 启动清理过期 IP 的协程
	go cleanupLoop()

	// 监听端口
	ln, err := net.Listen("tcp", config.ListenAddr)
	if err != nil {
		if config.ListenAddr == "" {
			log.Fatalf("Listen address is empty. Please configure 'listen_addr' in config.yaml or use -l flag.")
		}
		log.Fatalf("Error listening: %v", err)
	}
	log.Printf("Gate server locking on %s, protecting %s", config.ListenAddr, config.BackendAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

// normalizeIP 将 IPv4-mapped IPv6 地址（如 ::ffff:1.2.3.4）规范化为纯 IPv4 字符串
func normalizeIP(host string) string {
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func handleConnection(conn net.Conn) {
	// 获取 IP，规范化处理 IPv4-mapped IPv6 地址
	rawHost, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	host := normalizeIP(rawHost)

	// 设置读取超时，防止恶意连接占着茅坑不拉屎
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	buf := bufPool.Get().([]byte)
	defer bufPool.Put(buf)

	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return
	}

	// 重置 ReadDeadline
	conn.SetReadDeadline(time.Time{})

	payload := string(buf[:n])

	// 1. 判断是否是认证握手请求 (第一阶段：客户端发送 GATE_AUTH 请求 challenge)
	if payload == AuthPrefix || strings.TrimSpace(payload) == strings.TrimSuffix(AuthPrefix, ":") {
		handleAuthChallenge(conn, host)
		return
	}

	// 2. 判断是否是认证响应 (第二阶段：GATE_RESP:challenge:timestamp:signature)
	if strings.HasPrefix(payload, "GATE_RESP:") {
		handleAuthResponse(conn, payload, host)
		return
	}

	// 3. 如果不是验证包，检查白名单
	allowed, id := isWhitelisted(host)
	if allowed {
		log.Printf("Allowing connection from %s (ID: %s)", host, id)
		reportEvent(EventDecision, host, id, "允许", "白名单")
		proxyConnection(conn, buf[:n])
	} else {
		// 记录拒绝日志到单独文件
		msg := fmt.Sprintf("Blocking connection from %s (Not Whitelisted)", host)
		log.Println(msg) // 同时也打到主日志一份
		if blockLogger != nil {
			blockLogger.Println(msg)
		}
		reportEvent(EventDecision, host, "未知", "拦截", "未在白名单")
		conn.Close()
	}
}

// handleAuthChallenge 处理认证第一阶段：生成并发送 challenge
func handleAuthChallenge(conn net.Conn, ip string) {
	// 生成 32 字节随机 challenge
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		log.Printf("Failed to generate challenge for %s: %v", ip, err)
		conn.Close()
		return
	}
	challengeHex := hex.EncodeToString(challenge)

	// 注册 challenge，有效期 30 秒
	mu.Lock()
	pendingChallenges[challengeHex] = time.Now().Add(30 * time.Second)
	mu.Unlock()

	// 发送 challenge 给客户端
	if _, err := conn.Write([]byte("CHALLENGE:" + challengeHex)); err != nil {
		log.Printf("Write challenge error to %s: %v", ip, err)
	}
	conn.Close()
}

// handleAuthResponse 处理认证第二阶段：验证 challenge + timestamp 签名
func handleAuthResponse(conn net.Conn, payload string, ip string) {
	defer conn.Close()

	// 格式: GATE_RESP:challengeHex:timestamp:signatureHex
	parts := strings.SplitN(payload, ":", 4)
	if len(parts) != 4 {
		log.Printf("Auth response format error from %s", ip)
		return
	}

	challengeHex := parts[1]
	tsStr := parts[2]
	sigHex := parts[3]

	// 验证 challenge 是否有效（一次性消费）
	mu.Lock()
	expiry, exists := pendingChallenges[challengeHex]
	if !exists || time.Now().After(expiry) {
		if exists {
			delete(pendingChallenges, challengeHex)
		}
		mu.Unlock()
		log.Printf("Auth invalid/expired challenge from %s", ip)
		conn.Write([]byte("INVALID_CHALLENGE"))
		reportEvent(EventDecision, ip, "未知", "拦截", "无效挑战值")
		return
	}
	// 消费 challenge，防止重放
	delete(pendingChallenges, challengeHex)
	mu.Unlock()

	// 检查时间戳
	var ts int64
	_, err := fmt.Sscanf(tsStr, "%d", &ts)
	if err != nil {
		return
	}

	reqTime := time.Unix(ts, 0)
	if time.Since(reqTime).Abs() > 2*time.Minute {
		log.Printf("Auth timestamp expired from %s", ip)
		conn.Write([]byte("EXPIRED"))
		reportEvent(EventDecision, ip, "未知", "拦截", "鉴权过期")
		return
	}

	// 签名内容 = challenge_hex + ":" + timestamp
	msg := []byte(challengeHex + ":" + tsStr)
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		log.Printf("Auth signature invalid hex from %s", ip)
		reportEvent(EventDecision, ip, "未知", "拦截", "签名格式错误")
		return
	}

	valid, id := checkSignature(msg, sig)
	if valid {
		mu.Lock()
		// 检查IP绑定
		session, bound := whitelist[ip]
		if bound && time.Now().Before(session.ExpiresAt) && session.ID != id {
			mu.Unlock()
			log.Printf("Auth REJECTED for IP: %s (Locked by %s, tried %s)", ip, session.ID, id)
			reportEvent(EventDecision, ip, session.ID, "拦截", "IP已被其他玩家绑定")
			conn.Write([]byte("LOCKED"))
			return
		}

		whitelist[ip] = Session{
			ID:        id,
			ExpiresAt: time.Now().Add(time.Duration(config.AuthValidity)),
		}
		saveWhitelist()
		mu.Unlock()

		log.Printf("Auth SUCCESS for IP: %s (ID: %s)", ip, id)
		reportEvent(EventDecision, ip, id, "允许", "鉴权成功")
		conn.Write([]byte("OK"))
	} else {
		log.Printf("Auth FAILED for IP: %s (Crypto check failed)", ip)
		reportEvent(EventDecision, ip, "未知", "拦截", "签名校验失败")
		conn.Write([]byte("FAIL"))
	}
}

func checkSignature(msg, sig []byte) (bool, string) {
	for _, namedKey := range pubKeys {
		if verify(namedKey.Key, msg, sig) {
			return true, namedKey.ID
		}
	}
	return false, ""
}

func verify(pub crypto.PublicKey, msg, sig []byte) bool {
	switch k := pub.(type) {
	case ed25519.PublicKey:
		if len(sig) != ed25519.SignatureSize {
			return false
		}
		return ed25519.Verify(k, msg, sig)
	case *rsa.PublicKey:
		// RSA requires hashing the message first. Assuming SHA256.
		hashed := sha256.Sum256(msg)
		return rsa.VerifyPKCS1v15(k, crypto.SHA256, hashed[:], sig) == nil
	case *ecdsa.PublicKey:
		// ECDSA requires hashing too.
		hashed := sha256.Sum256(msg)
		return ecdsa.VerifyASN1(k, hashed[:], sig)
	default:
		return false
	}
}

func loadPublicKeys(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return loadSingleKey(path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := loadSingleKey(filepath.Join(path, entry.Name())); err != nil {
			log.Printf("Skipping invalid key file %s: %v", entry.Name(), err)
		}
	}
	return nil
}

func loadSingleKey(filePath string) error {
	// 只加载 .pub 公钥文件，跳过私钥及其他格式文件
	filename := filepath.Base(filePath)
	if !strings.HasSuffix(filename, ".pub") {
		return nil
	}

	keyBytes, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 从文件名 (wsj.pub -> wsj) 提取ID
	ext := filepath.Ext(filename)
	id := strings.TrimSuffix(filename, ext)

	// Try PEM decode
	block, _ := pem.Decode(keyBytes)
	if block != nil {
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err == nil {
			pubKeys = append(pubKeys, NamedKey{ID: id, Key: pub})
			return nil
		}
		// Try to see if it is a specific PEM type like RSA PUBLIC KEY (PKCS1)
		if block.Type == "RSA PUBLIC KEY" {
			if pub, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
				pubKeys = append(pubKeys, NamedKey{ID: id, Key: pub})
				return nil
			}
		}
		log.Printf("Failed to parse PEM public key %s: %v", filePath, err)
		return nil // don't fail, just skip?
	}

	// 简单解析，假设只有key内容 (Ed25519 Hex)
	keyStr := strings.TrimSpace(string(keyBytes))
	pubKey, err := hex.DecodeString(keyStr)
	if err != nil {
		// Not hex, not PEM
		return fmt.Errorf("failed to decode hex or pem")
	}

	if len(pubKey) == ed25519.PublicKeySize {
		pubKeys = append(pubKeys, NamedKey{ID: id, Key: ed25519.PublicKey(pubKey)})
		return nil
	}

	return fmt.Errorf("unknown key format or size in %s", filePath)
}

func addToWhitelist(ip, id string) {
	mu.Lock()
	defer mu.Unlock()

	whitelist[ip] = Session{
		ID:        id,
		ExpiresAt: time.Now().Add(time.Duration(config.AuthValidity)),
	}
	saveWhitelist()
}

func isWhitelisted(ip string) (bool, string) {
	mu.RLock()
	defer mu.RUnlock()

	session, ok := whitelist[ip]
	if !ok {
		return false, ""
	}

	if time.Now().After(session.ExpiresAt) {
		return false, ""
	}

	return true, session.ID
}

func cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	for range ticker.C {
		mu.Lock()
		now := time.Now()
		dirty := false
		for ip, session := range whitelist {
			if now.After(session.ExpiresAt) {
				delete(whitelist, ip)
				log.Printf("Session expired for %s", ip)
				dirty = true
			}
		}
		// 清理过期 challenge
		for ch, expiry := range pendingChallenges {
			if now.After(expiry) {
				delete(pendingChallenges, ch)
			}
		}
		if dirty {
			saveWhitelist()
		}
		mu.Unlock()
	}
}

func proxyConnection(src net.Conn, firstChunk []byte) {
	defer src.Close()

	if backend == "" {
		return
	}

	dest, err := net.Dial("tcp", backend)
	if err != nil {
		log.Printf("Failed to connect to backend %s: %v", backend, err)
		return
	}
	defer dest.Close()

	// Optimize TCP connection
	if tc, ok := src.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(3 * time.Minute)
	}
	if tc, ok := dest.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(3 * time.Minute)
	}

	// 1. 把预读的数据发给后端
	if len(firstChunk) > 0 {
		if _, err := dest.Write(firstChunk); err != nil {
			return
		}
	}

	// 2. 双向转发
	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(dest, src)
		errChan <- err
	}()

	go func() {
		_, err := io.Copy(src, dest)
		errChan <- err
	}()

	// 等待一侧关闭，主动中断两端让另一侧 goroutine 因 EOF 退出
	<-errChan
	src.Close()
	dest.Close()
	<-errChan
}
