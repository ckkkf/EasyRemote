// admin 二进制：只做装配与启动。
// 存储与会话由环境决定：DATABASE_URL → PG，REDIS_URL → Redis 会话。
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/easyremote/server/internal/adminapi"
	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/serve"
	"github.com/easyremote/server/internal/signal"
	"github.com/easyremote/server/internal/stack"
)

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	flag.Parse()

	ctx := context.Background()

	if err := config.ValidateDeployment(os.Getenv); err != nil {
		log.Fatalf("admin: %v", err)
	}
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
	var closer adminapi.SessionCloser
	var presence adminapi.Presence
	if url := os.Getenv("REDIS_URL"); url != "" {
		bus, busErr := signal.NewRedisBus(ctx, url, "admin")
		if busErr != nil {
			log.Fatalf("admin: signal bus: %v", busErr)
		}
		defer bus.Close()
		closer = signal.ConsentNotifier{Bus: bus}
		presence = signal.Presence{Bus: bus}
	}
	app, err := stack.BuildAdmin(st, sessions, limiter, replay, closer, presence)
	if err != nil {
		log.Fatalf("admin: %v", err)
	}
	log.Printf("admin listening on %s (store=%T)", *addr, st)
	if err := serve.Run(serve.Config{Name: "admin", Addr: *addr, Handler: app}); err != nil {
		log.Fatal(err)
	}
}
