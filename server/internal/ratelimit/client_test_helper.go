package ratelimit

import "github.com/redis/go-redis/v9"

// TestClient 暴露一个原始客户端给回归测试使用（用于造历史脏 key）。
func TestClient(url string) (*redis.Client, error) {
	return clientFromURL(url)
}
