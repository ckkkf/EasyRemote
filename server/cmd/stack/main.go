// stack 二进制：本地一体化进程。
// 设置 DATABASE_URL 时用 PostgreSQL（自动迁移），否则用内存存储。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/netdiag"
	"github.com/easyremote/server/internal/serve"
	"github.com/easyremote/server/internal/signal"
	"github.com/easyremote/server/internal/stack"
)

func main() {
	addr := flag.String("addr", ":8090", "listen address")
	stunAddr := flag.String("stun", "", "optional STUN UDP listen address, e.g. :3478")
	flag.Parse()

	ctx := context.Background()

	if *stunAddr != "" {
		stun, err := netdiag.Listen(*stunAddr)
		if err != nil {
			log.Fatalf("stack: stun listen failed: %v", err)
		}
		go func() {
			log.Printf("stun listening on %s", stun.Addr().String())
			if err := stun.Serve(ctx); err != nil {
				log.Printf("stun stopped: %v", err)
			}
		}()
		defer stun.Close()
	}

	redisURL := os.Getenv("REDIS_URL")

	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		if redisURL == "" && config.IsProduction() {
			// 生产没有共享 Bus：网关无法把授权请求送达被控端，信令也起不来。
			log.Fatal("stack: REDIS_URL is required in production (signalling + consent delivery)")
		}
		if redisURL != "" {
			secret, secErr := config.Secret()
			if secErr != nil {
				log.Fatalf("stack: %v", secErr)
			}
			bus, busErr := signal.NewRedisBus(ctx, redisURL, *addr+"-signal")
			if busErr != nil {
				log.Fatalf("stack: signal bus: %v", busErr)
			}
			defer bus.Close()
			hub := signal.NewHub()
			hub.SetInstanceID(*addr)
			hub.SetBus(bus)
			if err := bus.Subscribe(ctx, hub.DeliverLocal); err != nil {
				log.Fatalf("stack: signal subscribe: %v", err)
			}
			st, h, err := stack.NewPostgresWithRedis(ctx, dsn, redisURL, signal.ConsentNotifier{Bus: bus}, signal.Presence{Hub: hub, Bus: bus}, signal.ConsentNotifier{Bus: bus})
			if err != nil {
				log.Fatalf("stack: init failed: %v", err)
			}
			defer st.Close()
			checks := []stack.ReadyCheck{}
			if p, ok := st.(interface{ Ping(context.Context) error }); ok {
				checks = append(checks, stack.ReadyCheck{Name: "postgres", Code: errcode.SrvDbUnavailable, Fn: p.Ping})
			}
			checks = append(checks, stack.ReadyCheck{Name: "redis", Code: errcode.SrvCacheUnavailable, Fn: bus.Ping})
			h = stack.MountReady(h, checks...)
			rz, rzErr := stack.BuildRendezvousWithHub(st, secret, hub)
			if rzErr != nil {
				log.Fatalf("stack: rendezvous: %v", rzErr)
			}
			h = stack.MountRendezvous(h, rz)
			log.Printf("stack using postgres + redis sessions, listening on %s", *addr)
			if err := serve.Run(serve.Config{Name: "stack", Addr: *addr, Handler: h, DisableReadTimeout: true}); err != nil {
				log.Fatal(err)
			}
			return
		}
		st, h, err := stack.NewPostgres(ctx, dsn)
		if err != nil {
			log.Fatalf("stack: postgres init failed: %v", err)
		}
		defer st.Close()
		if p, ok := st.(interface{ Ping(context.Context) error }); ok {
			h = stack.MountReady(h, stack.ReadyCheck{Name: "postgres", Code: errcode.SrvDbUnavailable, Fn: p.Ping})
		}
		log.Printf("stack using postgres, listening on %s", *addr)
		if err := serve.Run(serve.Config{Name: "stack", Addr: *addr, Handler: h, DisableReadTimeout: true}); err != nil {
			log.Fatal(err)
		}
		return
	}

	// 一体化：先注册 hub，再建网关（网关的 Notifier 会引用它），最后挂信令
	secret, secErr := config.Secret()
	if secErr != nil {
		log.Fatalf("stack: %v", secErr)
	}
	hub := signal.NewHub()
	hub.SetInstanceID("stack")
	stack.SetDevHub(hub)

	mem, h, err := stack.NewDev()
	if err != nil {
		log.Fatal(err)
	}
	h = stack.MountReady(h)
	rz, rzErr := stack.BuildRendezvousWithHub(mem, secret, hub)
	if rzErr != nil {
		log.Fatalf("stack: rendezvous: %v", rzErr)
	}
	h = stack.MountRendezvous(h, rz)
	// E2E harness 显式 opt-in 的只读探针：只返回在线数，不暴露设备/账号。
	// 生产形态（cmd/gateway / DATABASE_URL 分支）没有这个路由。
	if os.Getenv("ER_E2E_HARNESS") == "1" {
		h = withE2EPresenceProbe(h, hub)
	}
	log.Printf("stack using in-memory store + signalling (set DATABASE_URL to use postgres), listening on %s", *addr)
	if err := serve.Run(serve.Config{Name: "stack", Addr: *addr, Handler: h, DisableReadTimeout: true}); err != nil {
		log.Fatal(err)
	}
}

// e2ePresence 只给 E2E harness 用的在线数探针。
type e2ePresence struct {
	Devices  int `json:"devices"`
	Sessions int `json:"sessions"`
}

// withE2EPresenceProbe 仅在 ER_E2E_HARNESS=1 时挂载：让测试等真实信令连接，
// 而不是用固定 sleep 猜「被控端应该已经上线」。
func withE2EPresenceProbe(next http.Handler, hub *signal.Hub) http.Handler {
	mux := http.NewServeMux()
	// 不用 "GET /path" 方法模式：go.mod 的 go directive 低于 1.22 时会被当成字面路径。
	mux.HandleFunc("/debug/presence", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpx.Err(w, r, errcode.New(errcode.SrvRouteNotFound, "method not allowed"))
			return
		}
		devices, sessions := hub.Counts()
		httpx.OK(w, r, e2ePresence{Devices: devices, Sessions: sessions})
	})
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &e2eRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		log.Printf("e2e http %s %s -> %d", r.Method, r.URL.Path, rec.status)
	}))
	return mux
}

// e2eRecorder 记录请求状态码；必须透传 Hijacker，否则 WebSocket 升级会 500。
type e2eRecorder struct {
	http.ResponseWriter
	status int
}

func (r *e2eRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *e2eRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack unsupported")
	}
	return h.Hijack()
}
