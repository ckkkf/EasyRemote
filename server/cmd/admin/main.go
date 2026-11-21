// admin 二进制：只做装配与启动。
// 存储与会话由环境决定：DATABASE_URL → PG，REDIS_URL → Redis 会话。
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/easyremote/server/internal/serve"
	"github.com/easyremote/server/internal/stack"
)

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	flag.Parse()

	ctx := context.Background()

	st, err := stack.OpenStore(ctx)
	if err != nil {
		log.Fatalf("admin: open store: %v", err)
	}
	defer st.Close()

	sessions, err := stack.OpenSessions(ctx, 8*time.Hour)
	if err != nil {
		log.Fatalf("admin: open sessions: %v", err)
	}

	limiter, err := stack.OpenLimiter(ctx)
	if err != nil {
		log.Fatalf("admin: open limiter: %v", err)
	}
	replay, err := stack.OpenReplayGuard(ctx)
	if err != nil {
		log.Fatalf("admin: open replay guard: %v", err)
	}
	app, err := stack.BuildAdmin(st, sessions, limiter, replay)
	if err != nil {
		log.Fatalf("admin: %v", err)
	}
	log.Printf("admin listening on %s (store=%T)", *addr, st)
	if err := serve.Run(serve.Config{Name: "admin", Addr: *addr, Handler: app}); err != nil {
		log.Fatal(err)
	}
}
