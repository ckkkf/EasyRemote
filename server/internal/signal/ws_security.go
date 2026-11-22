package signal

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// MaxWSMessageBytes 是单条信令消息上限。信令只传小 JSON（候选/状态），
// 放大到 1MB 只会给内存和解析器送攻击面。
const MaxWSMessageBytes = 64 << 10

// AllowedOriginsFromEnv 读取 ER_ALLOWED_ORIGINS（逗号分隔的精确 Origin）。
func AllowedOriginsFromEnv(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// OriginAllowed 判断 WebSocket Origin 是否允许。
//
//   - 空 Origin：原生客户端/Rust 不发 Origin，允许；
//   - 配置列表：精确匹配（scheme+host+port）；
//   - 未配置：只允许 localhost/127.0.0.1/::1（开发默认），绝不 CheckOrigin=true。
func OriginAllowed(origin string, allowed []string) bool {
	if origin == "" {
		return true
	}
	if len(allowed) > 0 {
		for _, a := range allowed {
			if origin == a {
				return true
			}
		}
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// WSAuthTimeoutFromEnv 读取未认证连接的 HELLO 截止时间（默认 5s）。
func WSAuthTimeoutFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("ER_WS_AUTH_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 5 * time.Second
}

// WSUpgrader 构造带 Origin allowlist 的 upgrader。
func WSUpgrader(allowed []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin: func(r *http.Request) bool {
			return OriginAllowed(r.Header.Get("Origin"), allowed)
		},
	}
}
