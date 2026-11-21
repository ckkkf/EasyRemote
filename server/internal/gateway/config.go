package gateway

import (
	"github.com/easyremote/server/internal/httpx"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/easyremote/server/internal/appconfig"
)

// clientConfig 把运行时配置下发给客户端。
//
// 只下发白名单键（见 internal/appconfig.Fields），且缺省键已填默认值：
// 客户端拿到的永远是「可用的值」，不需要在客户端再实现一遍默认逻辑。
// 需要登录态：配置决定会话时长与上传上限，属于账号维度的运行参数。
func (a *App) clientConfig(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if _, err := a.account(ctx, r); err != nil {
		WriteErr(w, err)
		return
	}
	cfg := a.Config
	if cfg == nil {
		cfg = appconfig.New(a.Store)
	}
	nested, err := cfg.Public(ctx)
	if err != nil {
		WriteErr(w, err)
		return
	}
	httpx.OK(w, r, map[string]any{"config": nested})
}

// reflect 回显服务端观察到的客户端出口地址。
//
// Web 端拿不到自己的公网地址，而 NAT 排查必须知道它：
// 这里返回「服务端看到的 IP:port」——注意端口是本次 TCP 连接的源端口，
// 不是 UDP 打洞用的端口，所以客户端必须把它标成「出口地址（TCP）」，不能冒充 NAT 探测结果。
func (a *App) netReflect(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	port := 0
	if host, p, err := splitHostPort(r.RemoteAddr); err == nil {
		ip, port = host, p
	}
	httpx.OK(w, r, map[string]any{
		"ip":      ip,
		"port":    port,
		"source":  "http-tcp",
		"via":     forwardedFor(r),
		"at_unix": nowUnix(),
	})
}

// splitHostPort 拆分 RemoteAddr；解析失败时由调用方原样保留客户端 IP。
func splitHostPort(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 0, err
	}
	port, convErr := strconv.Atoi(portStr)
	if convErr != nil {
		return host, 0, convErr
	}
	return host, port, nil
}

// forwardedFor 返回原始 X-Forwarded-For 头，便于判断是否经代理。
func forwardedFor(r *http.Request) string {
	return r.Header.Get("X-Forwarded-For")
}

func nowUnix() int64 { return time.Now().Unix() }
