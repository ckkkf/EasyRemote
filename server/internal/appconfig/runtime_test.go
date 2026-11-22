package appconfig

import (
	"testing"
)

func TestRuntimeEndpointsDevDefaults(t *testing.T) {
	t.Setenv("ER_PUBLIC_GATEWAY_URL", "")
	t.Setenv("ER_PUBLIC_SIGNAL_URL", "")
	t.Setenv("ER_PUBLIC_RELAY_ADDR", "")
	t.Setenv("ER_PUBLIC_TLS", "")
	ep, err := RuntimeEndpointsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if ep.GatewayURL == "" || ep.SignalURL == "" || ep.RelayAddr == "" {
		t.Fatalf("dev defaults missing: %+v", ep)
	}
	if ep.GatewayURL != ep.SignalURL {
		t.Fatalf("stack 默认 signal 应与 gateway 同源: %+v", ep)
	}
}

func TestRuntimeEndpointsProductionRequiresHTTPS(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_PUBLIC_GATEWAY_URL", "http://127.0.0.1:8080")
	t.Setenv("ER_PUBLIC_SIGNAL_URL", "http://127.0.0.1:8080")
	t.Setenv("ER_PUBLIC_RELAY_ADDR", "127.0.0.1:19000")
	if _, err := RuntimeEndpointsFromEnv(); err == nil {
		t.Fatal("production must reject localhost/http endpoints")
	}
}

func TestRuntimeEndpointsProductionExplicit(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_PUBLIC_GATEWAY_URL", "https://api.example.com")
	t.Setenv("ER_PUBLIC_SIGNAL_URL", "https://signal.example.com")
	t.Setenv("ER_PUBLIC_RELAY_ADDR", "relay.example.com:19000")
	t.Setenv("ER_PUBLIC_TLS", "1")
	ep, err := RuntimeEndpointsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if ep.TlsRequired != true || ep.GatewayURL != "https://api.example.com" {
		t.Fatalf("endpoints=%+v", ep)
	}
}
