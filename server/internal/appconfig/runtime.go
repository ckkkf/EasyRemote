package appconfig

import (
	"os"
	"strings"

	"github.com/easyremote/server/internal/config"
	"github.com/easyremote/server/internal/contract"
)

// RuntimeEndpointsFromEnv 返回客户端需要的统一运行时端点。
//
// 生产环境必须显式配置 gateway/signal/relay，且不得指向 localhost；
// 开发环境提供本机默认值，但发布构建不得依赖它们。
func RuntimeEndpointsFromEnv() (contract.RuntimeEndpoints, error) {
	out := contract.RuntimeEndpoints{
		GatewayURL:      strings.TrimSpace(os.Getenv("ER_PUBLIC_GATEWAY_URL")),
		SignalURL:       strings.TrimSpace(os.Getenv("ER_PUBLIC_SIGNAL_URL")),
		RelayAddr:       strings.TrimSpace(os.Getenv("ER_PUBLIC_RELAY_ADDR")),
		StunURL:         strings.TrimSpace(os.Getenv("ER_PUBLIC_STUN_URL")),
		WebtransportURL: strings.TrimSpace(os.Getenv("ER_PUBLIC_WEBTRANSPORT_URL")),
		TlsRequired:     truthy(os.Getenv("ER_PUBLIC_TLS")),
	}
	if out.GatewayURL == "" {
		out.GatewayURL = "http://127.0.0.1:8080"
	}
	if out.SignalURL == "" {
		out.SignalURL = out.GatewayURL
	}
	if out.RelayAddr == "" {
		out.RelayAddr = "127.0.0.1:19000"
	}
	if config.IsProduction() {
		if strings.Contains(out.GatewayURL, "127.0.0.1") || strings.Contains(out.GatewayURL, "localhost") ||
			strings.Contains(out.SignalURL, "127.0.0.1") || strings.Contains(out.SignalURL, "localhost") ||
			strings.Contains(out.RelayAddr, "127.0.0.1") || strings.Contains(out.RelayAddr, "localhost") {
			return contract.RuntimeEndpoints{}, errProductionEndpoint
		}
		if !strings.HasPrefix(out.GatewayURL, "https://") || !strings.HasPrefix(out.SignalURL, "https://") || !out.TlsRequired {
			return contract.RuntimeEndpoints{}, errProductionEndpoint
		}
	}
	return out, nil
}

var errProductionEndpoint = &endpointError{}

type endpointError struct{}

func (*endpointError) Error() string {
	return "appconfig: production endpoints must be explicit HTTPS URLs (no localhost)"
}
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
