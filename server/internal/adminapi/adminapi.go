// Package adminapi 提供管理后台业务。所有写操作强制落审计。
package adminapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"strings"
	"time"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/password"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// 角色常量，与页面清单 A14 对齐。
const (
	RoleSuper    = "super"
	RoleOps      = "ops"
	RoleCS       = "cs"
	RoleRisk     = "risk"
	RoleReadonly = "readonly"
)

// permission 定义每个动作允许的角色。
var permission = map[string][]string{
	"stats.read":   {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"user.read":    {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"user.ban":     {RoleSuper, RoleRisk},
	"device.read":  {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"session.read": {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"session.kill": {RoleSuper, RoleOps, RoleCS},
	"audit.read":   {RoleSuper, RoleOps, RoleRisk, RoleReadonly},
	// 配置中心决定会话时长、上传上限与客户端强更，只允许超级管理员改。
	"config.read":  {RoleSuper},
	"config.write": {RoleSuper},
}

// Service 是后台业务入口。
type Service struct {
	store    store.Store
	sessions *session.Service
	replay   ReplayGuard
	config   *appconfig.Service
	now      func() time.Time
	// devSkipTOTP 为真时跳过二次验证，仅供本地联调；生产装配会拒绝打开。
	devSkipTOTP bool
}

// ReplayGuard 记录已用过的 TOTP 时间步，防止同一验证码被重复使用。
// 多实例部署必须用共享实现（Redis）；单实例可用内存实现。
type ReplayGuard interface {
	// Use 标记 (subject, step) 已使用。
	// used=false 表示重放；err!=nil 表示后端不可用（调用方应返回 503 类错误）。
	Use(subject string, step int64) (used bool, err error)
}

// New 创建后台服务（默认内存重放防护，适用于单实例）。
func New(s store.Store, sess *session.Service) *Service {
	return NewWithReplay(s, sess, NewMemoryReplayGuard())
}

// NewWithReplay 注入指定的重放防护实现（多实例用 Redis 版本）。
func NewWithReplay(s store.Store, sess *session.Service, replay ReplayGuard) *Service {
	return &Service{store: s, sessions: sess, replay: replay, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// SetDevSkipTOTP 打开后跳过二次验证，仅开发环境可用。
// 生产装配必须拒绝打开，见 stack.adminMFADisabled。
func (s *Service) SetDevSkipTOTP(on bool) *Service {
	s.devSkipTOTP = on
	return s
}

// SetConfig 注入配置中心（P1-1）。未注入时按需用 store 构造。
func (s *Service) SetConfig(c *appconfig.Service) *Service {
	s.config = c
	return s
}

// configService 返回配置服务，惰性兜底避免装配遗漏导致 panic。
func (s *Service) configService() *appconfig.Service {
	if s.config == nil {
		s.config = appconfig.New(s.store)
	}
	return s.config
}

// Can 判断角色是否有权执行动作。
func Can(role, action string) bool {
	allowed, ok := permission[action]
	if !ok {
		return false
	}
	for _, r := range allowed {
		if r == role {
			return true
		}
	}
	return false
}

// Actor 是操作者上下文。
type Actor struct {
	Username string
	Role     string
	IP       string
}

// audit 写审计。写失败必须被发现——审计缺失比操作失败更严重。
// 这里返回 error，由调用方决定是否让请求失败（当前策略：写操作 fail-closed）。
func (s *Service) audit(ctx context.Context, a Actor, action, object, before, after, result string) error {
	return s.store.AppendAudit(ctx, &store.AuditLog{
		Actor:  a.Username,
		Role:   a.Role,
		Action: action,
		Object: object,
		Before: before,
		After:  after,
		IP:     a.IP,
		Result: result,
		TS:     s.now(),
	})
}

// Overview 返回仪表盘指标。权限校验放在 service 层，避免新增调用方时越权。
func (s *Service) Overview(ctx context.Context, a Actor) (contract.Overview, error) {
	if !Can(a.Role, "stats.read") {
		return contract.Overview{}, errcode.New(errcode.AdminForbidden, "stats.read")
	}
	all, err := s.collectDevices(ctx)
	if err != nil {
		return contract.Overview{}, err
	}
	devices := len(all)
	online := 0
	for _, d := range all {
		if d.Online {
			online++
		}
	}
	activeList, err := s.sessions.List(ctx, session.StateActive)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	pendingList, err := s.sessions.List(ctx, session.StateRequested)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	return contract.Overview{
		DevicesTotal:    devices,
		DevicesOnline:   online,
		SessionsActive:  len(activeList),
		SessionsPending: len(pendingList),
		GeneratedAt:     s.now(),
	}, nil
}

// ListUsers 返回脱敏账号信息。
func (s *Service) ListUsers(ctx context.Context, a Actor) ([]contract.UserRow, error) {
	if !Can(a.Role, "user.read") {
		return nil, errcode.New(errcode.AdminForbidden, "user.read")
	}
	accounts, err := s.collectAccounts(ctx)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.UserRow, 0, len(accounts))
	for _, acc := range accounts {
		out = append(out, contract.UserRow{
			ID:        acc.ID,
			PhoneMask: maskPhone(acc.PhoneEnc),
			Status:    acc.Status,
			CreatedAt: acc.CreatedAt,
		})
	}
	return out, nil
}

// collectAccounts 通过 Store 接口分页读取，不再依赖类型断言。
func (s *Service) collectAccounts(ctx context.Context) ([]*store.Account, error) {
	return s.store.ListAccounts(ctx, store.Page{Limit: 200})
}

func maskPhone(p string) string {
	if len(p) < 7 {
		return "***"
	}
	return p[:3] + "****" + p[len(p)-4:]
}

// BanUser 封禁账号，必须带原因。
func (s *Service) BanUser(ctx context.Context, a Actor, accountID, reason string) error {
	if !Can(a.Role, "user.ban") {
		_ = s.audit(ctx, a, "user.ban", accountID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "user.ban")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, "user.ban", accountID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	acc, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		_ = s.audit(ctx, a, "user.ban", accountID, "", "", "not_found")
		return errcode.New(errcode.AdminForbidden, "account not found")
	}
	before := acc.Status
	if err := s.store.UpdateAccountStatus(ctx, accountID, "banned"); err != nil {
		_ = s.audit(ctx, a, "user.ban", accountID, before, "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	if err := s.audit(ctx, a, "user.ban", accountID, before, "banned | "+reason, "ok"); err != nil {
		return errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return nil
}

// ListDevices 返回设备列表。
func (s *Service) ListDevices(ctx context.Context, a Actor) ([]contract.DeviceRow, error) {
	if !Can(a.Role, "device.read") {
		return nil, errcode.New(errcode.AdminForbidden, "device.read")
	}
	list, err := s.collectDevices(ctx)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.DeviceRow, 0, len(list))
	for _, d := range list {
		out = append(out, contract.DeviceRow{
			ID:         d.ID,
			DeviceCode: d.DeviceCode,
			AccountID:  d.AccountID,
			Platform:   d.Platform,
			Online:     d.Online,
			LastSeen:   d.LastSeenAt,
		})
	}
	return out, nil
}

func (s *Service) collectDevices(ctx context.Context) ([]*store.Device, error) {
	return s.store.ListDevices(ctx, store.Page{Limit: 500})
}

// ListSessions 返回会话列表。
func (s *Service) ListSessions(ctx context.Context, a Actor, state string) ([]contract.SessionRow, error) {
	if !Can(a.Role, "session.read") {
		return nil, errcode.New(errcode.AdminForbidden, "session.read")
	}
	list, err := s.sessions.List(ctx, state)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.SessionRow, 0, len(list))
	for _, sess := range list {
		out = append(out, contract.SessionRow{
			ID:           sess.ID,
			ControllerID: sess.ControllerID,
			TargetID:     sess.TargetID,
			State:        sess.State,
			Reason:       sess.Reason,
			StartedAt:    sess.StartedAt,
			EndedAt:      sess.EndedAt,
		})
	}
	return out, nil
}

// KillSession 强制中断，必须带原因。
func (s *Service) KillSession(ctx context.Context, a Actor, sessionID, reason string) error {
	if !Can(a.Role, "session.kill") {
		_ = s.audit(ctx, a, "session.kill", sessionID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "session.kill")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, "session.kill", sessionID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		_ = s.audit(ctx, a, "session.kill", sessionID, "", "", "not_found")
		return err
	}
	before := sess.State
	if _, err := s.sessions.Kill(ctx, sessionID, "admin: "+reason); err != nil {
		_ = s.audit(ctx, a, "session.kill", sessionID, before, "", "error")
		return err
	}
	if err := s.audit(ctx, a, "session.kill", sessionID, before, "closed | "+reason, "ok"); err != nil {
		return errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return nil
}

// Audit 返回审计日志。
func (s *Service) Audit(ctx context.Context, a Actor, limit int) ([]*store.AuditLog, error) {
	if !Can(a.Role, "audit.read") {
		return nil, errcode.New(errcode.AdminForbidden, "audit.read")
	}
	return s.store.ListAudit(ctx, limit)
}

// Authenticate 校验后台账号身份，不签发任何令牌（令牌由 adminsession 负责）。
// 口令错误与账号不存在统一返回同一错误码，避免枚举。
func (s *Service) Authenticate(ctx context.Context, username, password, totp string) (*store.AdminUser, error) {
	u, err := s.store.GetAdminByUsername(ctx, username)
	if err != nil {
		return nil, errcode.New(errcode.AuthInvalidCredential, "invalid credentials")
	}
	// 账号状态必须为 active，disabled/banned 一律拒绝
	if u.Status != "" && u.Status != "active" {
		return nil, errcode.New(errcode.AuthAccountBanned, "account disabled")
	}
	if !checkPassword(password, u.PasswordHash) {
		return nil, errcode.New(errcode.AuthInvalidCredential, "invalid credentials")
	}
	// 开发环境开关：跳过二次验证（生产装配禁止打开）
	if s.devSkipTOTP {
		return u, nil
	}
	// 强制 MFA：未配置 TOTP 的账号不得登录
	if u.TOTPSecret == "" {
		return nil, errcode.New(errcode.AdminMfaRequired, "mfa not configured")
	}
	step, ok := verifyTOTP(u.TOTPSecret, totp, s.now())
	if !ok {
		return nil, errcode.New(errcode.AdminMfaRequired, "invalid totp")
	}
	// 重放防护按 (管理员, 时间步) 计键：
	// 用全局时间步会让同一 30 秒内只有第一个管理员能登录（审查项 S2）。
	if s.replay != nil {
		used, rerr := s.replay.Use(u.ID, step)
		if rerr != nil {
			// 后端故障与服务不可用要区分开，不能伪装成「重放」
			return nil, errcode.New(errcode.SrvMaintenance, "mfa backend unavailable")
		}
		if !used {
			return nil, errcode.New(errcode.AdminMfaRequired, "totp replay")
		}
	}
	return u, nil
}

// AuditLogin 记录登录成功/失败，认证事件同样要进审计。
func (s *Service) AuditLogin(ctx context.Context, username, ip string, ok bool) {
	result := "ok"
	if !ok {
		result = "denied"
	}
	_ = s.store.AppendAudit(ctx, &store.AuditLog{
		Actor: username, Role: "-", Action: "admin.login", Object: username,
		IP: ip, Result: result, TS: s.now(),
	})
}

// checkPassword 优先用 argon2id 校验；历史 SHA256 哈希在迁移期仍可登录。
func checkPassword(plain, encoded string) bool {
	if password.IsEncoded(encoded) {
		return password.Verify(plain, encoded) == nil
	}
	return hmac.Equal([]byte(legacyHash(plain)), []byte(encoded))
}

func legacyHash(p string) string {
	m := sha256.New()
	m.Write([]byte("er-admin:" + p))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// HashPassword 供初始化管理员使用（argon2id）。
func HashPassword(p string) string {
	h, err := password.Hash(p)
	if err != nil {
		panic("adminapi: password hash failed: " + err.Error())
	}
	return h
}

// TOTPCode 按 RFC 6238 计算指定时刻的 6 位码（30s 步长）。
// 仅供开发环境与测试使用，生产由认证器 App 生成。
func TOTPCode(secret string, at time.Time) string {
	return hotp(decodeTOTPSecret(secret), at.UTC().Unix()/30)
}

// decodeTOTPSecret 把 base32 secret 解码；失败则按原样使用（兼容历史明文 secret）。
func decodeTOTPSecret(secret string) []byte {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	clean = strings.TrimRight(clean, "=")
	if b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(clean); err == nil {
		return b
	}
	return []byte(secret)
}

// totpStep 返回当前时间步（30s）与漂移窗口。
func totpStep(at time.Time) int64 { return at.UTC().Unix() / 30 }

// verifyTOTP 是 RFC 6238 的最小实现（30s 步长，6 位）。
func verifyTOTP(secret, code string, at time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	key := decodeTOTPSecret(secret)
	now := totpStep(at)
	for _, drift := range []int64{0, -1, 1} {
		step := now + drift
		if hotp(key, step) == code {
			return step, true
		}
	}
	return 0, false
}

func hotp(key []byte, counter int64) string {
	buf := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		buf[i] = byte(counter)
		counter >>= 8
	}
	m := hmac.New(sha256.New, key)
	m.Write(buf)
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (int(sum[off]&0x7f) << 24) | (int(sum[off+1]) << 16) | (int(sum[off+2]) << 8) | int(sum[off+3])
	return pad6(v % 1000000)
}

func pad6(n int) string {
	s := "000000" + itoa(n)
	return s[len(s)-6:]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
