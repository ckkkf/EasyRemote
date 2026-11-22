// gateway 二进制：只做装配与启动。
// 存储由环境决定：有 DATABASE_URL 用 PostgreSQL，否则开发环境退回内存。
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/gateway"
	"github.com/easyremote/server/internal/serve"
	"github.com/easyremote/server/internal/signal"
	"github.com/easyremote/server/internal/stack"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	ctx := context.Background()

	if err := config.ValidateDeployment(os.Getenv); err != nil {
		log.Fatalf("gateway: %v", err)
	}
	secret, err := config.Secret()
	if err != nil {
		log.Fatalf("gateway: %v", err)
	}
	st, err := stack.OpenStore(ctx)
	if err != nil {
		log.Fatalf("gateway: open store: %v", err)
	}
	defer st.Close()

	limiter, err := stack.OpenLimiter(ctx)
	if err != nil {
		log.Fatalf("gateway: open limiter: %v", err)
	}
	// 多实例通知：REDIS_URL 注入共享 Bus；进程内 Hub 只用于一体化开发。
	var notifier gateway.ConsentNotifier
	var presence gateway.Presence
	var closer gateway.SessionCloser
	if url := os.Getenv("REDIS_URL"); url != "" {
		bus, busErr := signal.NewRedisBus(ctx, url, "gateway")
		if busErr != nil {
			log.Fatalf("gateway: signal bus: %v", busErr)
		}
		defer bus.Close()
		n := signal.ConsentNotifier{Bus: bus}
		notifier = n
		presence = signal.Presence{Bus: bus}
		closer = n
	} else if config.IsProduction() {
		// 生产没有共享 Bus 时，建会话无法送达被控端，必须拒绝启动。
		log.Fatal("gateway: REDIS_URL is required in production (consent delivery)")
	}
	var epochs gateway.TicketEpochs
	if e, ok := notifier.(gateway.TicketEpochs); ok {
		epochs = e
	}
	app, err := stack.BuildGateway(st, secret, limiter, stack.DevSMSProvider(), notifier, presence, closer, epochs)
	if err != nil {
		log.Fatalf("gateway: %v", err)
	}
	log.Printf("gateway listening on %s (store=%T)", *addr, st)
	if err := serve.Run(serve.Config{Name: "gateway", Addr: *addr, Handler: app}); err != nil {
		log.Fatal(err)
	}
}
