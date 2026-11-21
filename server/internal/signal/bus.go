package signal

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// Bus 是跨实例消息总线。单实例可传 nil；多实例必须注入。
//
// 为什么需要：信令的在线表与会话绑定原本在本进程内，
// 两台设备若连到不同实例，就无法互相转发候选与授权结果。
type Bus interface {
	// Publish 把消息投递到目标设备所在的实例。
	Publish(ctx context.Context, deviceID string, msg Envelope) error
	// Subscribe 订阅本实例上的投递。
	Subscribe(ctx context.Context, handler func(deviceID string, msg Envelope) error) error

	// 会话绑定必须跨实例共享：主控连在实例 A、被控连在实例 B 时，
	// B 侧处理授权结果需要读到 A 侧建立的绑定。
	SaveBinding(ctx context.Context, b *Binding, ttl time.Duration) error
	LoadBinding(ctx context.Context, sessionID string) (*Binding, error)
	DeleteBinding(ctx context.Context, sessionID string) error

	// 在线表共享，实例崩溃后靠 TTL 自动过期。
	SetOnline(ctx context.Context, deviceID, instance string, ttl time.Duration) error
	ClearOnline(ctx context.Context, deviceID, instance string) error
	IsOnline(ctx context.Context, deviceID string) (bool, error)

	Close() error
}

// Envelope 是信令消息信封。
type Envelope struct {
	Type int             `json:"type"`
	Body json.RawMessage `json:"body"`
}

// RedisBus 用 Redis Pub/Sub 实现跨实例投递。
type RedisBus struct {
	rdb  *redis.Client
	self string
	sub  *redis.PubSub
}

var _ Bus = (*RedisBus)(nil)

// NewRedisBus 创建总线。self 为本实例标识，避免自己投给自己。
func NewRedisBus(ctx context.Context, url, self string) (*RedisBus, error) {
	rdb, err := redisClientFromURL(url)
	if err != nil {
		return nil, err
	}
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	return &RedisBus{rdb: rdb, self: self}, nil
}

func channel(deviceID string) string { return "er:signal:dev:" + deviceID }

// Publish 发到目标设备频道。
func (b *RedisBus) Publish(ctx context.Context, deviceID string, msg Envelope) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, channel(deviceID), raw).Err()
}

// Subscribe 订阅本实例的投递频道（按 deviceID 分发）。
func (b *RedisBus) Subscribe(ctx context.Context, handler func(deviceID string, msg Envelope) error) error {
	// 用通配订阅：消息里带 deviceID，便于本地 hub 二次过滤
	b.sub = b.rdb.PSubscribe(ctx, "er:signal:dev:*")
	go func() {
		for m := range b.sub.Channel() {
			deviceID := ""
			if idx := len("er:signal:dev:"); len(m.Channel) > idx {
				deviceID = m.Channel[idx:]
			}
			var env Envelope
			if err := json.Unmarshal([]byte(m.Payload), &env); err != nil {
				continue
			}
			_ = handler(deviceID, env)
		}
	}()
	return nil
}

// Close 关闭订阅与连接。
func (b *RedisBus) Close() error {
	if b.sub != nil {
		_ = b.sub.Close()
	}
	return b.rdb.Close()
}

func bindingKey(sessionID string) string { return "er:signal:bind:" + sessionID }

// SaveBinding 写入共享绑定。
func (b *RedisBus) SaveBinding(ctx context.Context, bind *Binding, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	key := bindingKey(bind.SessionID)
	pipe := b.rdb.TxPipeline()
	pipe.HSet(ctx, key, map[string]any{
		"from":       bind.From,
		"to":         bind.To,
		"created_at": bind.CreatedAt.Unix(),
	})
	// 必须设过期：否则会话结束后绑定永久驻留（审查项 S3）
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// LoadBinding 读取共享绑定。
func (b *RedisBus) LoadBinding(ctx context.Context, sessionID string) (*Binding, error) {
	data, err := b.rdb.HGetAll(ctx, bindingKey(sessionID)).Result()
	if err != nil || len(data) == 0 {
		return nil, ErrNotBound
	}
	created := int64(0)
	if v, ok := data["created_at"]; ok {
		for _, c := range v {
			if c < '0' || c > '9' {
				break
			}
			created = created*10 + int64(c-'0')
		}
	}
	return &Binding{
		SessionID: sessionID,
		From:      data["from"],
		To:        data["to"],
		CreatedAt: time.Unix(created, 0).UTC(),
	}, nil
}

// DeleteBinding 删除共享绑定。
func (b *RedisBus) DeleteBinding(ctx context.Context, sessionID string) error {
	return b.rdb.Del(ctx, bindingKey(sessionID)).Err()
}

// presenceKey 用于跨实例在线表。
func presenceKey(deviceID string) string { return "er:signal:online:" + deviceID }

// SetOnline 写入在线标记（带 TTL，实例崩溃后自动过期）。
func (b *RedisBus) SetOnline(ctx context.Context, deviceID, instance string, ttl time.Duration) error {
	return b.rdb.Set(ctx, presenceKey(deviceID), instance, ttl).Err()
}

// ClearOnline 仅当标记属于本实例时才删除。
func (b *RedisBus) ClearOnline(ctx context.Context, deviceID, instance string) error {
	const script = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`
	return b.rdb.Eval(ctx, script, []string{presenceKey(deviceID)}, instance).Err()
}

// IsOnline 查询设备是否在任意实例在线。
func (b *RedisBus) IsOnline(ctx context.Context, deviceID string) (bool, error) {
	n, err := b.rdb.Exists(ctx, presenceKey(deviceID)).Result()
	return n > 0, err
}
