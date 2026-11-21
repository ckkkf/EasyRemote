// udptun：UDP over TCP 隧道。
//
// 用途：Mac 与 Windows 不在同一网段时，让被控端仍能连到真实中继：
//
//	server 端（Mac）：-listen 127.0.0.1:19001 -target 127.0.0.1:9000
//	client 端（Windows）：-listen 127.0.0.1:19000 -server 127.0.0.1:19001
//
// 每个 TCP 连接对应一个独立的 UDP 源（中继按源地址回包）。
// 二进制只做搬运，不解析中继协议。
package main

import (
	"encoding/binary"
	"flag"
	"io"
	"log"
	"net"
	"sync"
)

func writeFrame(w io.Writer, b []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func serve(conn net.Conn, target string) {
	defer conn.Close()
	udp, err := net.Dial("udp", target)
	if err != nil {
		log.Printf("udp dial %s: %v", target, err)
		return
	}
	defer udp.Close()

	var once sync.Once
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := udp.Read(buf)
			if err != nil {
				once.Do(func() { close(done) })
				return
			}
			if err := writeFrame(conn, buf[:n]); err != nil {
				once.Do(func() { close(done) })
				return
			}
		}
	}()
	for {
		b, err := readFrame(conn)
		if err != nil {
			once.Do(func() { close(done) })
			return
		}
		if _, err := udp.Write(b); err != nil {
			once.Do(func() { close(done) })
			return
		}
	}
}

type session struct {
	conn net.Conn
	mu   sync.Mutex
}

func runServer(listen, target string) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen %s: %v", listen, err)
	}
	log.Printf("udptun server %s -> udp %s", listen, target)
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		log.Printf("session from %s", c.RemoteAddr())
		go serve(c, target)
	}
}

func runClient(listen, server string) {
	pc, err := net.ListenPacket("udp", listen)
	if err != nil {
		log.Fatalf("listen udp %s: %v", listen, err)
	}
	log.Printf("udptun client udp %s -> tcp %s", listen, server)
	sessions := make(map[string]*session)
	var mu sync.Mutex

	buf := make([]byte, 65535)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			log.Printf("udp read: %v", err)
			continue
		}
		key := addr.String()
		mu.Lock()
		s := sessions[key]
		mu.Unlock()
		if s == nil {
			c, err := net.Dial("tcp", server)
			if err != nil {
				log.Printf("tcp dial %s: %v", server, err)
				continue
			}
			s = &session{conn: c}
			mu.Lock()
			sessions[key] = s
			mu.Unlock()
			go func(s *session, peer net.Addr) {
				for {
					b, err := readFrame(s.conn)
					if err != nil {
						return
					}
					pc.WriteTo(b, peer)
				}
			}(s, addr)
			log.Printf("new uplink for %s", key)
		}
		s.mu.Lock()
		err = writeFrame(s.conn, buf[:n])
		s.mu.Unlock()
		if err != nil {
			log.Printf("tcp write: %v", err)
		}
	}
}

func main() {
	mode := flag.String("mode", "server", "server|client")
	listen := flag.String("listen", "127.0.0.1:19000", "listen address")
	target := flag.String("target", "127.0.0.1:9000", "server mode: UDP relay address")
	server := flag.String("server", "127.0.0.1:19001", "client mode: TCP server address")
	flag.Parse()
	switch *mode {
	case "server":
		runServer(*listen, *target)
	case "client":
		runClient(*listen, *server)
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
}
