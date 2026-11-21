package config

import "testing"

func TestSecretInDevelopmentFallsBack(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	t.Setenv("ER_SECRET", "")
	s, err := Secret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(s) == 0 {
		t.Fatal("empty secret")
	}
}

func TestSecretRequiredInProduction(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_SECRET", "")
	if _, err := Secret(); err == nil {
		t.Fatal("production without ER_SECRET must fail")
	}
}

func TestProductionRejectsDevSecret(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_SECRET", devSecret)
	if _, err := Secret(); err == nil {
		t.Fatal("production with dev secret must fail")
	}
}

func TestShortSecretRejected(t *testing.T) {
	t.Setenv("ER_ENV", "development")
	t.Setenv("ER_SECRET", "short")
	if _, err := Secret(); err == nil {
		t.Fatal("short secret must fail")
	}
}

func TestValidSecretAccepted(t *testing.T) {
	t.Setenv("ER_ENV", "production")
	t.Setenv("ER_SECRET", "0123456789abcdef0123")
	s, err := Secret()
	if err != nil || len(s) == 0 {
		t.Fatalf("valid secret rejected: %v", err)
	}
}
