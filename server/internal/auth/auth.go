// Package auth 负责账号、短信验证码与访问令牌。
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

// SMSProvider 是短信通道抽象。测试用固定码实现，生产接真实网关。
// Verify 由实现方决定校验规则（真实网关通常回查缓存）。
type SMSProvider interface {
	Send(phone, code string) error
	Verify(phone, code string) error
}

// Service 提供登录与令牌能力。
type Service struct {
	store    store.Store
	sms      SMSProvider
	email    EmailProvider
	secret   []byte
	now      func() time.Time
	tokenTTL time.Duration
}

// New 创建 auth 服务。secret 为空时拒绝启动（避免生产误配默认密钥）。
func New(s store.Store, sms SMSProvider, secret []byte) (*Service, error) {
	if len(secret) == 0 {
		return nil, errors.New("auth: empty token secret")
	}
	return &Service{store: s, sms: sms, secret: secret, now: func() time.Time { return time.Now().UTC() }, tokenTTL: 30 * 24 * time.Hour}, nil
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// SetEmailProvider 注入邮件通道。不注入时走开发固定码。
func (s *Service) SetEmailProvider(p EmailProvider) { s.email = p }

// phoneHash 用 HMAC 而非裸 SHA256，避免手机号被彩虹表反查。
func (s *Service) phoneHash(phone string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("phone:" + phone))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// SendCode 发送登录验证码。生产需接真实通道；本期由调用方注入实现。
func (s *Service) SendCode(phone string) error {
	if len(phone) < 6 {
		return errcode.New(errcode.AuthInvalidCredential, "phone too short")
	}
	code := "123456" // 由 SMSProvider 决定真实码；这里只做通道调用
	if s.sms != nil {
		if err := s.sms.Send(phone, code); err != nil {
			return errcode.New(errcode.AuthSmsRateLimited, err.Error())
		}
	}
	return nil
}

type tokenPayload struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

func (s *Service) sign(p tokenPayload) (string, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	m := hmac.New(sha256.New, s.secret)
	m.Write(body)
	sig := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(body) + "." + sig, nil
}

// Login 校验验证码并签发令牌。首次登录自动建号。
func (s *Service) Login(ctx context.Context, phone, code string) (string, *store.Account, error) {
	if phone == "" || code == "" {
		return "", nil, errcode.New(errcode.AuthInvalidCredential, "empty phone or code")
	}
	if s.sms != nil {
		if err := s.sms.Verify(phone, code); err != nil {
			return "", nil, errcode.New(errcode.AuthInvalidCredential, "sms verify failed")
		}
	} else if code != "123456" {
		// 无 SMSProvider 时使用固定测试码，明确标注避免误入生产
		return "", nil, errcode.New(errcode.AuthInvalidCredential, "invalid code")
	}

	hash := s.phoneHash(phone)
	acc, err := s.store.GetAccountByPhoneHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		acc = &store.Account{PhoneHash: hash, Status: "active"}
		if err := s.store.CreateAccount(ctx, acc); err != nil {
			return "", nil, errcode.New(errcode.SrvInternal, err.Error())
		}
	} else if err != nil {
		return "", nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	if acc.Status == "banned" {
		return "", nil, errcode.New(errcode.AuthAccountBanned, acc.ID)
	}

	tok, err := s.sign(tokenPayload{Sub: acc.ID, Exp: s.now().Add(s.tokenTTL).Unix()})
	if err != nil {
		return "", nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return tok, acc, nil
}

// DevEmailCode 是开发环境邮箱登录的固定验证码。
//
// 与短信通道一样：生产环境必须由 EmailProvider 注入真实投递与校验，
// 常量码只在本机/测试生效，避免「开发后门」被带上线。
const DevEmailCode = "654321"

// EmailProvider 是邮件验证码通道抽象。
// Verify 由实现方决定校验规则（真实通道通常回查缓存或一次性令牌）。
type EmailProvider interface {
	Send(email, code string) error
	Verify(email, code string) error
}

// NormalizeEmail 归一化邮箱并做基本格式校验。
// 归一化很重要：Alice@Example.com 与 alice@example.com 必须是同一个账号。
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if len(e) < 5 || len(e) > 254 {
		return "", errcode.New(errcode.AuthInvalidCredential, "email length invalid")
	}
	at := strings.IndexByte(e, '@')
	if at <= 0 || at == len(e)-1 {
		return "", errcode.New(errcode.AuthInvalidCredential, "email missing @")
	}
	domain := e[at+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", errcode.New(errcode.AuthInvalidCredential, "email domain invalid")
	}
	if strings.ContainsAny(e, " \t\r\n") {
		return "", errcode.New(errcode.AuthInvalidCredential, "email contains whitespace")
	}
	return e, nil
}

// emailHash 与手机号使用同一套 HMAC，但加不同的命名空间前缀，
// 因此手机号哈希与邮箱哈希绝不会在 accounts.phone_hash 上撞车。
func (s *Service) emailHash(email string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("email:" + email))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// SendEmailCode 触发一次邮箱验证码投递。
// 未注入 EmailProvider 时不发信，只做邮箱格式校验（开发环境固定码 DevEmailCode）。
func (s *Service) SendEmailCode(email string) error {
	if _, err := NormalizeEmail(email); err != nil {
		return err
	}
	if s.email != nil {
		normalized, _ := NormalizeEmail(email)
		if err := s.email.Send(normalized, DevEmailCode); err != nil {
			return errcode.New(errcode.AuthSmsSendFailed, err.Error())
		}
	}
	return nil
}

// LoginEmail 校验邮箱验证码并签发令牌。邮箱不存在则建号。
// 令牌与手机号登录同构（同一签名密钥、同一 payload、同一 TTL）。
func (s *Service) LoginEmail(ctx context.Context, email, code string) (string, *store.Account, error) {
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return "", nil, err
	}
	if code == "" {
		return "", nil, errcode.New(errcode.AuthInvalidCredential, "empty code")
	}
	if s.email != nil {
		if err := s.email.Verify(normalized, code); err != nil {
			return "", nil, errcode.New(errcode.AuthInvalidCredential, "email code verify failed")
		}
	} else if code != DevEmailCode {
		return "", nil, errcode.New(errcode.AuthInvalidCredential, "invalid email code")
	}

	hash := s.emailHash(normalized)
	acc, err := s.store.GetAccountByPhoneHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		acc = &store.Account{PhoneHash: hash, PhoneEnc: normalized, Status: "active"}
		if err := s.store.CreateAccount(ctx, acc); err != nil {
			return "", nil, errcode.New(errcode.SrvInternal, err.Error())
		}
	} else if err != nil {
		return "", nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	if acc.Status == "banned" {
		return "", nil, errcode.New(errcode.AuthAccountBanned, acc.ID)
	}
	tok, err := s.sign(tokenPayload{Sub: acc.ID, Exp: s.now().Add(s.tokenTTL).Unix()})
	if err != nil {
		return "", nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return tok, acc, nil
}

// VerifyToken 校验令牌并返回账号。
func (s *Service) VerifyToken(ctx context.Context, tok string) (*store.Account, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		return nil, errcode.New(errcode.AuthTokenExpired, "malformed token")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errcode.New(errcode.AuthTokenExpired, "bad payload")
	}
	m := hmac.New(sha256.New, s.secret)
	m.Write(body)
	want := base64.RawURLEncoding.EncodeToString(m.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return nil, errcode.New(errcode.AuthTokenExpired, "bad signature")
	}
	var p tokenPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, errcode.New(errcode.AuthTokenExpired, "bad json")
	}
	if s.now().Unix() > p.Exp {
		return nil, errcode.New(errcode.AuthTokenExpired, "expired")
	}
	acc, err := s.store.GetAccount(ctx, p.Sub)
	if err != nil {
		return nil, errcode.New(errcode.AuthTokenExpired, "account gone")
	}
	if acc.Status == "banned" {
		return nil, errcode.New(errcode.AuthAccountBanned, acc.ID)
	}
	return acc, nil
}
