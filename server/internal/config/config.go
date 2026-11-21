// Package config 负责从环境读取配置，并对生产环境做 fail-fast 校验。
package config

import (
	"errors"
	"os"
	"strings"
)

// devSecret 仅允许在非生产环境使用。
const devSecret = "dev-secret-change-me"

// Env 返回归一化后的运行环境："production" 或 "development"。
// 支持 production / prod（大小写、空白均可）；未知值一律按生产处理（fail-safe）。
func Env() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ER_ENV")))
	switch v {
	case "", "dev", "development", "local", "test":
		return "development"
	default:
		// 包括 production / prod / 拼写错误：一律按生产处理，
		// 避免 "ER_ENV=prod" 之类静默退化为内存模式。
		return "production"
	}
}

// IsProduction 判断是否为生产环境。
func IsProduction() bool { return Env() == "production" }

// Secret 返回 HMAC 密钥。生产必须通过 ER_SECRET 注入，否则拒绝启动。
func Secret() ([]byte, error) {
	v := os.Getenv("ER_SECRET")
	if v == "" {
		if IsProduction() {
			return nil, errors.New("config: ER_SECRET is required in production")
		}
		return []byte(devSecret), nil
	}
	if IsProduction() && v == devSecret {
		return nil, errors.New("config: refusing to start in production with the development secret")
	}
	if len(v) < 16 {
		return nil, errors.New("config: ER_SECRET must be at least 16 bytes")
	}
	return []byte(v), nil
}
