package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestValidateDeploymentProductionComplete(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	m := map[string]string{
		"DATABASE_URL":          "postgres://db/easyremote",
		"REDIS_URL":             "redis://cache:6379",
		"ER_SECRET":             "0123456789abcdef0123456789abcdef",
		"ER_PUBLIC_GATEWAY_URL": "https://api.example.com",
		"ER_PUBLIC_SIGNAL_URL":  "https://signal.example.com",
		"ER_PUBLIC_RELAY_ADDR":  "relay.example.com:19000",
		"ER_PUBLIC_TLS":         "1",
	}
	if err := ValidateDeployment(env(m)); err != nil {
		t.Fatalf("complete production config rejected: %v", err)
	}
}

func TestValidateDeploymentRejectsIncompleteProduction(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	base := map[string]string{
		"DATABASE_URL":          "postgres://db/easyremote",
		"REDIS_URL":             "redis://cache:6379",
		"ER_SECRET":             "0123456789abcdef0123456789abcdef",
		"ER_PUBLIC_GATEWAY_URL": "https://api.example.com",
		"ER_PUBLIC_SIGNAL_URL":  "https://signal.example.com",
		"ER_PUBLIC_RELAY_ADDR":  "relay.example.com:19000",
		"ER_PUBLIC_TLS":         "1",
	}
	for _, key := range []string{"DATABASE_URL", "REDIS_URL", "ER_SECRET", "ER_PUBLIC_GATEWAY_URL", "ER_PUBLIC_SIGNAL_URL", "ER_PUBLIC_RELAY_ADDR", "ER_PUBLIC_TLS"} {
		m := map[string]string{}
		for k, v := range base {
			if k != key {
				m[k] = v
			}
		}
		if err := ValidateDeployment(env(m)); err == nil {
			t.Fatalf("missing %s must fail", key)
		}
	}
}

func TestValidateDeploymentRejectsDebugSwitchesInProduction(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	m := map[string]string{
		"DATABASE_URL":          "postgres://db/easyremote",
		"REDIS_URL":             "redis://cache:6379",
		"ER_SECRET":             "0123456789abcdef0123456789abcdef",
		"ER_PUBLIC_GATEWAY_URL": "http://127.0.0.1:8080",
		"ER_PUBLIC_SIGNAL_URL":  "http://127.0.0.1:8080",
		"ER_PUBLIC_RELAY_ADDR":  "127.0.0.1:19000",
		"ER_PUBLIC_TLS":         "0",
		"ER_ADMIN_TOTP_OFF":     "1",
	}
	if err := ValidateDeployment(env(m)); err == nil {
		t.Fatal("localhost/http/debug switches must fail in production")
	}
}

// 限流开关与 TOTP 开关同级别：生产环境配了就必须拒绝启动。
func TestValidateDeploymentRejectsRateLimitOffInProduction(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	m := map[string]string{
		"DATABASE_URL":          "postgres://db/easyremote",
		"REDIS_URL":             "redis://cache:6379",
		"ER_SECRET":             "0123456789abcdef0123456789abcdef",
		"ER_PUBLIC_GATEWAY_URL": "https://api.example.com",
		"ER_PUBLIC_SIGNAL_URL":  "https://signal.example.com",
		"ER_PUBLIC_RELAY_ADDR":  "relay.example.com:19000",
		"ER_PUBLIC_TLS":         "1",
		"ER_RATELIMIT_OFF":      "1",
	}
	err := ValidateDeployment(env(m))
	if err == nil || !strings.Contains(err.Error(), "ER_RATELIMIT_OFF") {
		t.Fatalf("want ER_RATELIMIT_OFF refusal, got %v", err)
	}
}

// 开发环境允许打开限流开关（本地联调不锁人）。
func TestValidateDeploymentDevAllowsRateLimitOff(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	t.Setenv("ER_RATELIMIT_OFF", "1")
	if err := ValidateDeployment(env(map[string]string{"ER_RATELIMIT_OFF": "1"})); err != nil {
		t.Fatalf("dev should allow ER_RATELIMIT_OFF: %v", err)
	}
}

func TestValidateDeploymentDevAllowsDefaults(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	if err := ValidateDeployment(env(map[string]string{})); err != nil {
		t.Fatalf("dev should not require production config: %v", err)
	}
}
