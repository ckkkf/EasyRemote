package config

import (
	"errors"
	"strings"
)

// ValidateDeployment 校验生产部署的配置完整性。
//
// 生产必须显式提供数据库、Redis/总线、密钥与公开端点；任何半配置
// （例如只配了 gateway 没配 relay）都直接失败，禁止半工作启动。
func ValidateDeployment(getenv func(string) string) error {
	if !IsProduction() {
		return nil
	}
	required := map[string]string{
		"DATABASE_URL":          getenv("DATABASE_URL"),
		"REDIS_URL":             getenv("REDIS_URL"),
		"ER_SECRET":             getenv("ER_SECRET"),
		"ER_PUBLIC_GATEWAY_URL": getenv("ER_PUBLIC_GATEWAY_URL"),
		"ER_PUBLIC_SIGNAL_URL":  getenv("ER_PUBLIC_SIGNAL_URL"),
		"ER_PUBLIC_RELAY_ADDR":  getenv("ER_PUBLIC_RELAY_ADDR"),
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return errors.New("config: " + name + " is required in production")
		}
	}
	gateway := strings.TrimSpace(required["ER_PUBLIC_GATEWAY_URL"])
	signal := strings.TrimSpace(required["ER_PUBLIC_SIGNAL_URL"])
	relay := strings.TrimSpace(required["ER_PUBLIC_RELAY_ADDR"])
	for _, endpoint := range []string{gateway, signal} {
		if !strings.HasPrefix(endpoint, "https://") {
			return errors.New("config: public gateway/signal endpoints must be https in production")
		}
		if strings.Contains(endpoint, "127.0.0.1") || strings.Contains(endpoint, "localhost") {
			return errors.New("config: public endpoints must not be localhost in production")
		}
	}
	if strings.Contains(relay, "127.0.0.1") || strings.Contains(relay, "localhost") {
		return errors.New("config: public relay address must not be localhost in production")
	}
	if !truthy(getenv("ER_PUBLIC_TLS")) {
		return errors.New("config: ER_PUBLIC_TLS must be enabled in production")
	}
	if truthy(getenv("ER_ADMIN_TOTP_OFF")) {
		return errors.New("config: ER_ADMIN_TOTP_OFF is forbidden in production")
	}
	if getenv("ER_SECRET") == devSecret {
		return errors.New("config: development secret is forbidden in production")
	}
	return nil
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
