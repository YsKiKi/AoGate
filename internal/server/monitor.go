package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// EventType 定义事件类型
type EventType string

const (
	EventConnect  EventType = "connect"
	EventDecision EventType = "decision"
)

// MonitorEvent 发送给 WS 服务器的消息结构
type MonitorEvent struct {
	Type      EventType `json:"type"`
	Timestamp int64     `json:"ts"`
	IP        string    `json:"ip"`
	ID        string    `json:"id"`     // 玩家ID
	Action    string    `json:"action"` // "allowed", "blocked", "connecting"
	Reason    string    `json:"reason,omitempty"`
}

var (
	wsURL    string
	wsSendCh chan MonitorEvent
)

func initMonitor(targetAddr string) {
	// C2: monitor_addr 为空时完全禁用监控，不建立任何 WebSocket 连接
	if targetAddr == "" {
		log.Printf("Monitor: disabled (monitor_addr not configured)")
		return
	}

	// 智能判断地址格式
	if strings.Contains(targetAddr, "://") {
		wsURL = targetAddr
	} else if strings.Contains(targetAddr, ":") {
		// 包含冒号，认为是 host:port
		wsURL = fmt.Sprintf("ws://%s", targetAddr)
	} else {
		// 不含冒号，认为是纯端口，默认 localhost
		wsURL = fmt.Sprintf("ws://127.0.0.1:%s", targetAddr)
	}

	// 安全警告：非 wss:// 连接的监控数据以明文传输
	if !strings.HasPrefix(wsURL, "wss://") {
		log.Printf("Monitor: WARNING - using unencrypted WebSocket (ws://). Consider using wss:// for production.")
	}

	wsSendCh = make(chan MonitorEvent, 100)

	// 启动 WS 客户端守护协程
	go wsClientLoop()
}

func reportEvent(eventType EventType, ip string, id string, action string, reason string) {
	// C2: 监控未启用时直接返回
	if wsSendCh == nil {
		return
	}
	event := MonitorEvent{
		Type:      eventType,
		Timestamp: time.Now().Unix(),
		IP:        ip,
		ID:        id,
		Action:    action,
		Reason:    reason,
	}

	// 非阻塞发送，满了就丢弃
	select {
	case wsSendCh <- event:
	default:
		// Channel full, drop event
	}
}

func wsClientLoop() {
	for {
		// 1. 尝试连接
		// log.Printf("Monitor: Connecting to %s...", wsURL)
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			// 连接失败，等待后重试
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("Monitor: Connected to %s", wsURL)

		// 2. 发送循环
		done := make(chan struct{})

		// 读取循环 (用于处理 Close 消息或 Ping/Pong)
		go func() {
			defer close(done)
			for {
				_, _, err := c.ReadMessage()
				if err != nil {
					return
				}
			}
		}()

		monitorLoop(c, done)
		c.Close()
		log.Printf("Monitor: Connection lost, reconnecting...")
		time.Sleep(3 * time.Second)
	}
}

func monitorLoop(c *websocket.Conn, done chan struct{}) {
	for {
		select {
		case <-done:
			return
		case event := <-wsSendCh:
			if err := c.WriteJSON(event); err != nil {
				return
			}
		}
	}
}
