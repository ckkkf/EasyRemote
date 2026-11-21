package serve

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestRunStartsAndResponds(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	done := make(chan error, 1)
	go func() {
		done <- Run(Config{Name: "test", Addr: addr, Handler: h, ReadTimeout: time.Second})
	}()

	var resp *http.Response
	for i := 0; i < 30; i++ {
		resp, err = http.Get("http://" + addr + "/")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server did not start: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
}
