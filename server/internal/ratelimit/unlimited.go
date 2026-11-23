package ratelimit

import (
	"context"
	"time"
)

// Unlimited 是本地开发用的限流器：永远放行。
//
// 为什么需要它：生产限流（登录 10 次/小时、后台登录 5 次/10 分钟、短信 1 次/分钟、
// 邮件验证码 1 次/分钟）是按真实用户行为定的，本地联调会在几十秒内反复登录/发码，
// 开发者被自己锁在门外，只能靠重启进程清内存计数——那不是配置，是掩盖问题。
//
// 它只能由 ER_RATELIMIT_OFF 显式开启，且生产环境禁止：
// config.ValidateDeployment 与 stack.OpenLimiter 都会拒绝启动。
type Unlimited struct{}

// NewUnlimited 创建开发用无限流器。
func NewUnlimited() *Unlimited { return &Unlimited{} }

var _ Limiter = (*Unlimited)(nil)

// Allow 永远放行，不计窗口。
func (u *Unlimited) Allow(context.Context, string, int, time.Duration) (Result, error) {
	return Result{Allowed: true, Remaining: 0, RetryAfter: 0}, nil
}

// Reset 无状态，直接成功。
func (u *Unlimited) Reset(context.Context, string) error { return nil }

// Ping 无外部依赖，直接成功。
func (u *Unlimited) Ping(context.Context) error { return nil }
