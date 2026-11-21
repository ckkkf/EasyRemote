// lossyproxy：用户态 UDP 丢包/时延/乱序注入代理。
//
// 为什么不用 tc netem：需要 sudo，且会污染整机网络；这里只影响被测 UDP 流。
// 位置：被测端（Windows）与真实中继之间，双向注入，参数化。
//
// 用法：
//
//	lossyproxy -listen 127.0.0.1:19100 -target 127.0.0.1:19000 -loss 0.05 -delay 5ms -reorder 0.02
//
// 每个下游源地址对应一条独立的上游 socket（中继按源地址识别端位）。
package main

import (
	"flag"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type stats struct {
	downIn, downDrop, upIn, upDrop atomic.Int64
}

type upstream struct {
	conn   *net.UDPConn
	client *net.UDPAddr
}

func main() {
	listen := flag.String("listen", "127.0.0.1:19100", "UDP listen (被测端连这里)")
	target := flag.String("target", "127.0.0.1:19000", "UDP target (真实中继)")
	loss := flag.Float64("loss", 0, "丢包比例 0..1（双向各算一次）")
	delay := flag.Duration("delay", 0, "单向固定时延")
	jitter := flag.Duration("jitter", 0, "时延抖动上限（随机 0..jitter 叠加）")
	reorder := flag.Float64("reorder", 0, "乱序概率：命中则额外延迟 40ms")
	seed := flag.Int64("seed", 1, "随机种子（保证可复现）")
	lossFile := flag.String("loss-file", "", "可选：文件里写丢包比例，每秒重读一次（用于会话中途切换，验证 ABR 升档）")
	flag.Parse()

	if *lossFile != "" {
		go func() {
			for range time.Tick(time.Second) {
				b, err := os.ReadFile(*lossFile)
				if err != nil {
					continue
				}
				v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
				if err == nil && v >= 0 && v <= 1 {
					*loss = v
				}
			}
		}()
	}

	rng := rand.New(rand.NewSource(*seed))
	var mu sync.Mutex // 保护 rng 与 sessions
	sessions := make(map[string]*upstream)
	st := &stats{}

	var targetAddr *net.UDPAddr
	{
		a, err := net.ResolveUDPAddr("udp", *target)
		if err != nil {
			log.Fatalf("resolve target: %v", err)
		}
		targetAddr = a
	}

	laddr, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		log.Fatalf("resolve listen: %v", err)
	}
	pc, err := net.ListenUDP("udp", laddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	_ = pc.SetReadBuffer(4 << 20)
	log.Printf("lossyproxy %s -> %s loss=%.3f delay=%v jitter=%v reorder=%.3f seed=%d",
		*listen, *target, *loss, *delay, *jitter, *reorder, *seed)

	shouldDrop := func() bool {
		if *loss <= 0 {
			return false
		}
		mu.Lock()
		v := rng.Float64()
		mu.Unlock()
		return v < *loss
	}
	extraDelay := func() time.Duration {
		d := *delay
		if *jitter > 0 {
			mu.Lock()
			j := time.Duration(rng.Int63n(int64(*jitter) + 1))
			mu.Unlock()
			d += j
		}
		if *reorder > 0 {
			mu.Lock()
			hit := rng.Float64() < *reorder
			mu.Unlock()
			if hit {
				d += 40 * time.Millisecond
			}
		}
		return d
	}

	// 统计
	go func() {
		for range time.Tick(2 * time.Second) {
			log.Printf("stats down_in=%d down_drop=%d up_in=%d up_drop=%d",
				st.downIn.Load(), st.downDrop.Load(), st.upIn.Load(), st.upDrop.Load())
		}
	}()

	// 下游 -> 上游
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := pc.ReadFromUDP(buf)
			if err != nil {
				log.Printf("read down: %v", err)
				continue
			}
			st.downIn.Add(1)
			key := addr.String()
			mu.Lock()
			up := sessions[key]
			mu.Unlock()
			if up == nil {
				c, err := net.DialUDP("udp", nil, targetAddr)
				if err != nil {
					log.Printf("dial target: %v", err)
					continue
				}
				_ = c.SetReadBuffer(4 << 20)
				up = &upstream{conn: c, client: addr}
				mu.Lock()
				sessions[key] = up
				mu.Unlock()
				go pumpBack(up, pc, st, shouldDrop, extraDelay, &mu, rng)
				log.Printf("new session %s", key)
			}
			if shouldDrop() {
				st.downDrop.Add(1)
				continue
			}
			b := append([]byte(nil), buf[:n]...)
			d := extraDelay()
			if d <= 0 {
				_, _ = up.conn.Write(b)
				continue
			}
			time.AfterFunc(d, func() { _, _ = up.conn.Write(b) })
		}
	}()

	select {}
}

// pumpBack 处理 上游(中继) -> 下游(被测端) 方向。
func pumpBack(up *upstream, down *net.UDPConn, st *stats, shouldDrop func() bool, extraDelay func() time.Duration, _ *sync.Mutex, _ *rand.Rand) {
	buf := make([]byte, 65535)
	for {
		n, err := up.conn.Read(buf)
		if err != nil {
			log.Printf("upstream read: %v", err)
			return
		}
		st.upIn.Add(1)
		if shouldDrop() {
			st.upDrop.Add(1)
			continue
		}
		b := append([]byte(nil), buf[:n]...)
		d := extraDelay()
		if d <= 0 {
			_, _ = down.WriteToUDP(b, up.client)
			continue
		}
		client := up.client
		time.AfterFunc(d, func() { _, _ = down.WriteToUDP(b, client) })
	}
}
