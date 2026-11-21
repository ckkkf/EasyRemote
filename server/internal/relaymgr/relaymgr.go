// Package relaymgr 负责中继票据的签发与校验。
// 约束：票据绑定「本端设备」，一人一票，避免共享票据被第三方抢夺端位。
package relaymgr

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/easyremote/server/internal/errcode"
)

// Ticket 是单个设备使用的中继凭证。
type Ticket struct {
	SessionID string
	Self      string // 持票设备
	Peer      string // 对端设备
	ExpiresAt time.Time
}

// Service 签发与校验票据。
type Service struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// New 创建票据服务。密钥为空时返回错误由调用方处理（见 Validate）。
func New(secret []byte, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	return &Service{secret: secret, ttl: ttl, now: func() time.Time { return time.Now().UTC() }}
}

// Validate 检查服务是否可用（空密钥拒绝服务）。
func (s *Service) Validate() error {
	if len(s.secret) < 16 {
		return errors.New("relaymgr: secret must be at least 16 bytes")
	}
	return nil
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

func (s *Service) mac(payload string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Issue 为 self 设备签发一张指向 peer 的票据。
func (s *Service) Issue(sessionID, self, peer string) (string, *Ticket, error) {
	if sessionID == "" || self == "" || peer == "" {
		return "", nil, errcode.New(errcode.SrvInternal, "issue: empty field")
	}
	if err := s.Validate(); err != nil {
		return "", nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	exp := s.now().Add(s.ttl)
	payload := strings.Join([]string{sessionID, self, peer, strconv.FormatInt(exp.Unix(), 10)}, "|")
	token := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + s.mac(payload)
	return token, &Ticket{SessionID: sessionID, Self: self, Peer: peer, ExpiresAt: exp}, nil
}

// Verify 校验票据。
func (s *Service) Verify(token string) (*Ticket, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errcode.New(errcode.ConnRelayUnavailable, "malformed ticket")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errcode.New(errcode.ConnRelayUnavailable, "bad payload")
	}
	payload := string(raw)
	if !hmac.Equal([]byte(s.mac(payload)), []byte(parts[1])) {
		return nil, errcode.New(errcode.ConnRelayUnavailable, "bad signature")
	}
	f := strings.Split(payload, "|")
	if len(f) != 4 {
		return nil, errcode.New(errcode.ConnRelayUnavailable, "bad fields")
	}
	exp, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil {
		return nil, errcode.New(errcode.ConnRelayUnavailable, "bad exp")
	}
	if s.now().Unix() > exp {
		return nil, errcode.New(errcode.ConnRelayUnavailable, "ticket expired")
	}
	return &Ticket{SessionID: f[0], Self: f[1], Peer: f[2], ExpiresAt: time.Unix(exp, 0).UTC()}, nil
}
