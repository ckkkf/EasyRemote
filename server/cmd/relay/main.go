// relay 是 L4 UDP 中继：按票据把密文在两端之间双向转发。
// 约束：不解析载荷；票据绑定设备身份；未登记来源一律丢弃。
//
// 多节点：设置 REDIS_URL 后启用节点目录与路由表，
// 两端注册到不同节点时，数据经节点间转发互达。
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"time"

	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/relay"
	"github.com/easyremote/server/internal/relaymgr"
)

// loadSecret 复用统一配置：生产缺 ER_SECRET 或仍是 dev 默认值时直接失败。
func loadSecret() ([]byte, error) {
	return config.Secret()
}

var dbgReg, dbgData, dbgDrop int

func main() {
	addr := flag.String("addr", ":9000", "udp listen address")
	nodeAddr := flag.String("node-addr", "", "address other nodes use to reach this node (defaults to listen address)")
	flag.Parse()

	secret, err := loadSecret()
	if err != nil {
		log.Fatal(err)
	}
	tickets := relaymgr.New(secret, time.Hour)
	if err := tickets.Validate(); err != nil {
		log.Fatalf("relay: %v", err)
	}

	uaddr, err := net.ResolveUDPAddr("udp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", uaddr)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	self := *nodeAddr
	if self == "" {
		self = *addr
	}

	fwd := relay.NewForwarder()

	// 多节点目录
	var reg relay.Registry
	if url := os.Getenv("REDIS_URL"); url != "" {
		r, err := relay.NewRedisRegistryFromURL(ctx, url)
		if err != nil {
			log.Fatalf("relay: registry: %v", err)
		}
		defer r.Close()
		reg = r
		fwd.SetRegistry(reg, self)
		go func() {
			for {
				if err := reg.RegisterNode(ctx, self, self, 30*time.Second); err != nil {
					log.Printf("relay: register node: %v", err)
				}
				time.Sleep(10 * time.Second)
			}
		}()
	}

	log.Printf("relay listening on %s (node=%s)", *addr, self)
	go func() {
		for range time.Tick(2 * time.Second) {
			log.Printf("relay stats: register=%d data=%d drop=%d", dbgReg, dbgData, dbgDrop)
		}
	}()

	buf := make([]byte, relay.MaxDatagram)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("read: %v", err)
			continue
		}
		kind, sid, body, err := relay.ParseFrame(buf[:n])
		if err != nil {
			continue
		}
		switch kind {
		case relay.FrameRegister:
			dbgReg++
			dev, tok, err := relay.ParseRegister(body)
			if err != nil {
				continue
			}
			tk, err := tickets.Verify(tok)
			if err != nil {
				log.Printf("relay: verify failed sid=%s dev=%s err=%v", sid, dev, err)
				continue
			}
			if !fwd.Register(tk, dev, from) {
				log.Printf("relay: rejected register sid=%s dev=%s", sid, dev)
				continue
			}
			if reg != nil {
				_ = reg.SetRoute(ctx, tk.SessionID, dev, self, 2*time.Minute)
			}

		case relay.FrameData:
			dbgData++
			plan, err := fwd.Plan(ctx, sid, from)
			if err != nil {
				dbgDrop++
				log.Printf("relay: plan failed sid=%s from=%s err=%v", sid, from, err)
				continue
			}
			if plan.RemoteNode != "" {
				if reg == nil {
					continue
				}
				target, err := reg.LookupNode(ctx, plan.RemoteNode)
				if err != nil {
					continue
				}
				dst, err := net.ResolveUDPAddr("udp", target)
				if err != nil {
					continue
				}
				// 跨节点帧：带上目标设备，由对端节点投递
				dev := fwd.PeerDeviceOf(sid, from)
				frame := relay.EncodeFrame(relay.FrameNodeForward, sid, relay.EncodeNodeForward(dev, body))
				_, _ = conn.WriteToUDP(frame, dst)
				continue
			}
			_, _ = conn.WriteToUDP(body, &plan.Local)

		case relay.FrameNodeForward:
			dev, payload, err := relay.ParseNodeForward(body)
			if err != nil {
				continue
			}
			if dst := fwd.LocalAddrOf(sid, dev); dst != nil {
				_, _ = conn.WriteToUDP(payload, dst)
			}
		}
	}
}
