// stack 二进制：本地一体化进程。
// 设置 DATABASE_URL 时用 PostgreSQL（自动迁移），否则用内存存储。
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/easyremote/server/internal/config"
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
		if redisURL != "" {
			st, h, err := stack.NewPostgresWithRedis(ctx, dsn, redisURL)
			if err != nil {
				log.Fatalf("stack: init failed: %v", err)
			}
			defer st.Close()
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
	rz, rzErr := stack.BuildRendezvousWithHub(mem, secret, hub)
	if rzErr != nil {
		log.Fatalf("stack: rendezvous: %v", rzErr)
	}
	h = stack.MountRendezvous(h, rz)
	log.Printf("stack using in-memory store + signalling (set DATABASE_URL to use postgres), listening on %s", *addr)
	if err := serve.Run(serve.Config{Name: "stack", Addr: *addr, Handler: h, DisableReadTimeout: true}); err != nil {
		log.Fatal(err)
	}
}
