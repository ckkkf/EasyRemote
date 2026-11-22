package config

import (
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

func TestValidateDeploymentDevAllowsDefaults(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	if err := ValidateDeployment(env(map[string]string{})); err != nil {
		t.Fatalf("dev should not require production config: %v", err)
	}
}
