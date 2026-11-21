// Package password 提供后台账号口令的哈希与校验（argon2id + per-user salt）。
// 旧实现是裸 SHA256，无 salt、无成本参数，库泄露后秒级还原（审查项 S7）。
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// 参数取 OWASP 推荐下限的偏保守值，登录取 50ms 量级。
	timeCost    uint32 = 3
	memoryCost  uint32 = 64 * 1024 // 64 MiB
	parallelism uint8  = 2
	keyLen      uint32 = 32
	saltLen     int    = 16
)

// ErrMismatch 表示口令不匹配。
var ErrMismatch = errors.New("password: mismatch")

// Hash 生成 `argon2id$v=19$m=...,t=...,p=...$salt$hash` 格式的字符串。
func Hash(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(plain), salt, timeCost, memoryCost, parallelism, keyLen)
	return fmt.Sprintf("argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryCost, timeCost, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

// Verify 校验口令。使用常量时间比较，参数从存储串解析，便于日后调参。
func Verify(plain, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return ErrMismatch
	}
	var version int
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil || version != argon2.Version {
		return ErrMismatch
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return ErrMismatch
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return ErrMismatch
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return ErrMismatch
	}
	got := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// IsEncoded 判断字符串是否已是 argon2id 格式（用于启动时校验种子账号）。
func IsEncoded(s string) bool { return strings.HasPrefix(s, "argon2id$") }
