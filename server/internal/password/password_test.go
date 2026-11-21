package password

import (
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h, err := Hash("dev-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncoded(h) {
		t.Fatalf("unexpected format: %s", h)
	}
	if err := Verify("dev-pass", h); err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if err := Verify("wrong", h); err != ErrMismatch {
		t.Fatalf("want mismatch, got %v", err)
	}
}

func TestHashesAreSalted(t *testing.T) {
	a, _ := Hash("same")
	b, _ := Hash("same")
	if a == b {
		t.Fatal("hashes must differ (per-user salt)")
	}
	if err := Verify("same", a); err != nil {
		t.Fatal(err)
	}
	if err := Verify("same", b); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsMalformed(t *testing.T) {
	for _, s := range []string{"", "plain", "sha256$abc", "argon2id$v=19$bad$x$y"} {
		if Verify("x", s) != ErrMismatch {
			t.Fatalf("malformed %q should mismatch", s)
		}
	}
}

func TestFormatContainsParams(t *testing.T) {
	h, _ := Hash("x")
	if !strings.Contains(h, "m=65536") || !strings.Contains(h, "t=3") {
		t.Fatalf("params missing: %s", h)
	}
}
