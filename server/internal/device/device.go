// Package device 负责设备注册、设备码分配与一次性验证码。
package device

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
)

const (
	codeTTL       = 10 * time.Minute
	maxAttempts   = 5
	lockDuration  = 30 * time.Minute
	trustDuration = 7 * 24 * time.Hour
)

// Service 提供设备码与验证码能力。
type Service struct {
	store  store.Store
	secret []byte
	now    func() time.Time
}

// New 创建设备服务。
func New(s store.Store, secret []byte) *Service {
	return &Service{store: s, secret: secret, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// FormatCode 把 9 位数字格式化为 3-3-3 展示。
func FormatCode(raw string) string {
	if len(raw) != 9 {
		return raw
	}
	return raw[0:3] + " " + raw[3:6] + " " + raw[6:9]
}

func normalize(code string) string { return strings.ReplaceAll(strings.TrimSpace(code), " ", "") }

// allocateCode 生成不冲突的 9 位设备码。
func (s *Service) allocateCode(ctx context.Context) (string, error) {
	for i := 0; i < 20; i++ {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		n := binary.BigEndian.Uint64(buf[:]) % 900000000
		code := fmt.Sprintf("%09d", 100000000+n)
		if _, err := s.store.GetDeviceByCode(ctx, code); errors.Is(err, store.ErrNotFound) {
			return code, nil
		}
	}
	return "", errcode.New(errcode.SrvInternal, "code allocation exhausted")
}

// Bind 为账号绑定一台设备，返回设备码与一次性设备凭证。
// 设备凭证只在绑定响应中出现一次，后续用于「仅本机可做」的操作。
func (s *Service) Bind(ctx context.Context, accountID, platform string) (*store.Device, string, error) {
	if accountID == "" {
		return nil, "", errcode.New(errcode.AuthTokenExpired, "no account")
	}
	code, err := s.allocateCode(ctx)
	if err != nil {
		return nil, "", err
	}
	deviceToken, err := randomToken()
	if err != nil {
		return nil, "", errcode.New(errcode.SrvInternal, err.Error())
	}
	d := &store.Device{
		DeviceCode: code,
		AccountID:  accountID,
		Platform:   platform,
		LastSeenAt: s.now(),
		Online:     true,
		TokenHash:  s.hashToken(deviceToken),
	}
	if err := s.store.CreateDevice(ctx, d); err != nil {
		return nil, "", errcode.New(errcode.SrvInternal, err.Error())
	}
	return d, deviceToken, nil
}

func randomToken() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func (s *Service) hashToken(token string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("dt:" + token))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// VerifyDeviceToken 校验设备凭证是否属于该设备（恒定时间比较）。
func (s *Service) VerifyDeviceToken(ctx context.Context, deviceID, token string) bool {
	if deviceID == "" || token == "" {
		return false
	}
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil || d.TokenHash == "" {
		return false
	}
	return hmac.Equal([]byte(d.TokenHash), []byte(s.hashToken(token)))
}

// RotateToken 重新签发设备凭证（例如设备重置）。
func (s *Service) RotateToken(ctx context.Context, deviceID string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", errcode.New(errcode.SrvInternal, err.Error())
	}
	if err := s.store.UpdateDeviceToken(ctx, deviceID, s.hashToken(token)); err != nil {
		return "", errcode.New(errcode.SrvInternal, err.Error())
	}
	return token, nil
}

// hashPassword 与设备凭证同一套 HMAC：泄露库也拿不到明文密码。
func (s *Service) hashPassword(deviceID, password string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("dp:" + deviceID + ":" + password))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// SetPassword 设置/覆盖设备临时密码。明文由被控端本地保存（可查看、可复制），
// 服务端只存哈希，且**不做轮换、不做消费**——这是与一次性验证码最大的区别。
func (s *Service) SetPassword(ctx context.Context, deviceID, password string) error {
	if len(password) < 6 || len(password) > 16 {
		return errcode.New(errcode.ConnCaptchaInvalid, "password length must be 6..16")
	}
	for _, r := range password {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') {
			return errcode.New(errcode.ConnCaptchaInvalid, "password must be alphanumeric")
		}
	}
	return s.store.UpdateDevicePassword(ctx, deviceID, s.hashPassword(deviceID, password))
}

// CheckPassword 校验临时密码（恒定时间比较，不消费）。
func (s *Service) CheckPassword(ctx context.Context, deviceCode, password string) bool {
	d, err := s.store.GetDeviceByCode(ctx, normalize(deviceCode))
	if err != nil || d.PasswordHash == "" || password == "" {
		return false
	}
	return hmac.Equal([]byte(d.PasswordHash), []byte(s.hashPassword(d.ID, password)))
}

func (s *Service) hashCode(deviceCode, code string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("vc:" + deviceCode + ":" + code))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// RotateCode 轮换被控端的临时验证码，返回明文（仅此刻可见，落库只存哈希）。
func (s *Service) RotateCode(ctx context.Context, deviceCode string) (string, time.Time, error) {
	deviceCode = normalize(deviceCode)
	if _, err := s.store.GetDeviceByCode(ctx, deviceCode); err != nil {
		return "", time.Time{}, errcode.New(errcode.ConnDeviceNotFound, deviceCode)
	}
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", time.Time{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	n := binary.BigEndian.Uint32(buf[:]) % 1000000
	code := fmt.Sprintf("%06d", n)
	exp := s.now().Add(codeTTL)
	if err := s.store.UpsertVerifyCode(ctx, &store.VerifyCode{
		DeviceCode: deviceCode,
		CodeHash:   s.hashCode(deviceCode, code),
		ExpiresAt:  exp,
	}); err != nil {
		return "", time.Time{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	return code, exp, nil
}

// VerifyCode 校验一次性验证码。校验与消费在存储层原子完成，
// 因此并发爆破既不会丢计数，也无法把一个码用两次。
func (s *Service) VerifyCode(ctx context.Context, deviceCode, code string) error {
	deviceCode = normalize(deviceCode)
	res, err := s.store.VerifyAndConsume(ctx, deviceCode, s.hashCode(deviceCode, code), s.now(), maxAttempts, lockDuration)
	if err != nil {
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	switch res.Outcome {
	case store.VerifyOK:
		return nil
	case store.VerifyLocked:
		return errcode.New(errcode.ConnCaptchaLocked, fmt.Sprintf("locked until %s", res.LockedTo.Format(time.RFC3339)))
	case store.VerifyExpired:
		return errcode.New(errcode.ConnCaptchaExpired, "no valid code")
	default:
		return errcode.New(errcode.ConnCaptchaInvalid, fmt.Sprintf("attempt %d/%d", res.Attempts, maxAttempts))
	}
}

// GrantTrust 记录信任关系，后续同账号直连可跳过验证码。
func (s *Service) GrantTrust(ctx context.Context, controllerID, targetID string) error {
	return s.store.UpsertTrust(ctx, &store.TrustEdge{
		ControllerID: controllerID,
		TargetID:     targetID,
		ExpiresAt:    s.now().Add(trustDuration),
	})
}

// HasTrust 查询信任关系。
func (s *Service) HasTrust(ctx context.Context, controllerID, targetID string) bool {
	ok, err := s.store.HasTrust(ctx, controllerID, targetID, s.now())
	return err == nil && ok
}

// List 返回账号下的设备。
func (s *Service) List(ctx context.Context, accountID string) ([]*store.Device, error) {
	return s.store.ListDevicesByAccount(ctx, accountID, store.Page{Limit: 200})
}
