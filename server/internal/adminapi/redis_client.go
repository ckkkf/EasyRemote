package adminapi

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// redisClient 解析 redis:// 或 host:port 形式地址。
func redisClient(raw string) (*redis.Client, error) {
	addr, password, db := raw, "", 0
	if strings.HasPrefix(raw, "redis://") || strings.HasPrefix(raw, "rediss://") {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}
		addr = u.Host
		if u.User != nil {
			password, _ = u.User.Password()
			if password == "" {
				password = u.User.Username()
			}
		}
		if p := strings.TrimPrefix(u.Path, "/"); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				db = n
			}
		}
	}
	return redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db}), nil
}
