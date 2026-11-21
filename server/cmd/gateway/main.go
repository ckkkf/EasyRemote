// gateway 二进制：只做装配与启动。
// 存储由环境决定：有 DATABASE_URL 用 PostgreSQL，否则开发环境退回内存。
package main

import (
	"context"
	"flag"
	"log"

	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/serve"
	"github.com/easyremote/server/internal/stack"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	ctx := context.Background()

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
	app, err := stack.BuildGateway(st, secret, limiter, stack.DevSMSProvider())
	if err != nil {
		log.Fatalf("gateway: %v", err)
	}
	log.Printf("gateway listening on %s (store=%T)", *addr, st)
	if err := serve.Run(serve.Config{Name: "gateway", Addr: *addr, Handler: app}); err != nil {
		log.Fatal(err)
	}
}
