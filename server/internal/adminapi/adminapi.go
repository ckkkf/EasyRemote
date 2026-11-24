// Package adminapi 提供管理后台业务。所有写操作强制落审计。
package adminapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sort"
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
	"stats.read": {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"user.read":  {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"user.ban":   {RoleSuper, RoleRisk},
	// 账号备注（ADM-API-03c）：客服/运营/风控都要能记，但只读角色不行。
	"user.write":  {RoleSuper, RoleOps, RoleCS, RoleRisk},
	"device.read": {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	// 强制下线会断掉该设备参与的会话：客服可用；解绑是破坏性操作，只给超级管理员。
	"device.force_offline": {RoleSuper, RoleOps, RoleCS},
	"device.unbind":        {RoleSuper},
	// 风险标记是风控/运营的判断，不涉及破坏性操作。
	"device.flag_risk":  {RoleSuper, RoleRisk, RoleOps},
	"device.clear_risk": {RoleSuper, RoleRisk, RoleOps},
	"session.read":      {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"session.kill":      {RoleSuper, RoleOps, RoleCS},
	"audit.read":        {RoleSuper, RoleOps, RoleRisk, RoleReadonly},
	// 配置中心决定会话时长、上传上限与客户端强更，只允许超级管理员改。
	"config.read":  {RoleSuper},
	"config.write": {RoleSuper},
	// A14 管理员与角色：只给超管；写操作必须填原因（ADM-API-06a）。
	"admin.read":  {RoleSuper},
	"admin.write": {RoleSuper},
	// A11 版本管理：运营可读可写，只读角色只能看。
	"version.read":  {RoleSuper, RoleOps, RoleCS, RoleRisk, RoleReadonly},
	"version.write": {RoleSuper, RoleOps},
	// A10 风控：风控角色可读写，其余只读（或不可见）。
	"risk.read":  {RoleSuper, RoleRisk, RoleOps, RoleReadonly},
	"risk.write": {RoleSuper, RoleRisk},
}

// Service 是后台业务入口。
type Service struct {
	store    store.Store
	sessions *session.Service
	replay   ReplayGuard
	config   *appconfig.Service
	closer   SessionCloser
	presence Presence
	now      func() time.Time
	// devSkipTOTP 为真时跳过二次验证，仅供本地联调；生产装配会拒绝打开。
	devSkipTOTP bool
}

// SessionCloser 把后台强制中断传播给会话双方。
type SessionCloser interface {
	NotifySessionClosed(sessionID, from, to, reason string) error
}

// Presence 是权威在线判定（生产为 signal.Presence）。
type Presence interface {
	Online(ctx context.Context, deviceID string) (bool, error)
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

// SetSessionCloser 注入关闭传播器（生产为 Bus notifier）。
func (s *Service) SetSessionCloser(c SessionCloser) *Service {
	s.closer = c
	return s
}

// SetPresence 注入权威在线判定；未注入时回落设备表字段（仅开发）。
func (s *Service) SetPresence(p Presence) *Service {
	s.presence = p
	return s
}

func (s *Service) online(ctx context.Context, d *store.Device) bool {
	if s.presence == nil {
		return d.Online
	}
	ok, err := s.presence.Online(ctx, d.ID)
	if err != nil {
		return false
	}
	return ok
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
// Permissions 返回该角色可执行的动作清单（用于登录响应与前端只读控制）。
func Permissions(role string) []string {
	out := []string{}
	for action, roles := range permission {
		for _, r := range roles {
			if r == role {
				out = append(out, action)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

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
	// 在线用户 = 至少有一台在线设备的账号数（按账号去重，不是设备数也不是登录数）。
	onlineAccounts := make(map[string]struct{})
	for _, d := range all {
		if s.online(ctx, d) {
			online++
			onlineAccounts[d.AccountID] = struct{}{}
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
		UsersOnline:     len(onlineAccounts),
		SessionsActive:  len(activeList),
		SessionsPending: len(pendingList),
		GeneratedAt:     s.now(),
	}, nil
}

// ListUsers 返回脱敏账号信息。
func (s *Service) ListUsers(ctx context.Context, a Actor, page store.Page) ([]contract.UserRow, error) {
	if !Can(a.Role, "user.read") {
		return nil, errcode.New(errcode.AdminForbidden, "user.read")
	}
	accounts, err := s.store.ListAccounts(ctx, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	ids := make([]string, 0, len(accounts))
	for _, acc := range accounts {
		ids = append(ids, acc.ID)
	}
	// 一次查出本页所有账号的设备数，避免每行一次查询（ADM-API-03a）。
	counts, err := s.store.DeviceCountsByAccount(ctx, ids)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.UserRow, 0, len(accounts))
	for _, acc := range accounts {
		row := contract.UserRow{
			ID:          acc.ID,
			Nickname:    acc.Nickname,
			PhoneMask:   maskPhone(acc.PhoneEnc),
			Status:      acc.Status,
			DeviceCount: counts[acc.ID],
			CreatedAt:   acc.CreatedAt,
		}
		// 零值表示从未登录：与 SessionRow.ended_at 同约定，序列化成 0001-01-01，UI 显示为「—」。
		row.LastLoginAt = acc.LastLoginAt
		out = append(out, row)
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
func (s *Service) ListDevices(ctx context.Context, a Actor, page store.Page) ([]contract.DeviceRow, error) {
	if !Can(a.Role, "device.read") {
		return nil, errcode.New(errcode.AdminForbidden, "device.read")
	}
	list, err := s.store.ListDevices(ctx, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.DeviceRow, 0, len(list))
	for _, d := range list {
		out = append(out, contract.DeviceRow{
			ID:            d.ID,
			DeviceCode:    d.DeviceCode,
			AccountID:     d.AccountID,
			Platform:      d.Platform,
			DeviceName:    d.DeviceName,
			OsVersion:     d.OSVersion,
			ClientVersion: d.ClientVersion,
			NatType:       d.NATType,
			RiskFlag:      d.RiskFlag,
			RiskReason:    d.RiskReason,
			Online:        s.online(ctx, d),
			LastSeen:      d.LastSeenAt,
		})
	}
	return out, nil
}

func (s *Service) collectDevices(ctx context.Context) ([]*store.Device, error) {
	return s.store.ListDevices(ctx, store.Page{Limit: 500})
}

// ListSessions 返回会话列表。
func (s *Service) ListSessions(ctx context.Context, a Actor, state string, page store.Page) ([]contract.SessionRow, error) {
	if !Can(a.Role, "session.read") {
		return nil, errcode.New(errcode.AdminForbidden, "session.read")
	}
	list, err := s.store.ListSessions(ctx, state, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	// 会话级质量摘要（ADM-API-02f7）：批量查一次，避免 N+1。
	ids := make([]string, 0, len(list))
	for _, sess := range list {
		ids = append(ids, sess.ID)
	}
	summaries, serr := s.store.SessionSummaries(ctx, ids)
	if serr != nil {
		return nil, errcode.New(errcode.SrvInternal, serr.Error())
	}
	out := make([]contract.SessionRow, 0, len(list))
	for _, sess := range list {
		row := contract.SessionRow{
			ID:           sess.ID,
			ControllerID: sess.ControllerID,
			TargetID:     sess.TargetID,
			State:        sess.State,
			Reason:       sess.Reason,
			StartedAt:    sess.StartedAt,
			EndedAt:      sess.EndedAt,
		}
		if sum, ok := summaries[sess.ID]; ok && sum.Samples > 0 {
			row.LatencyAvailable = true
			row.AvgLatencyMs = sum.AvgLatencyMs
			row.Link = dominantLink(sum.P2PCount, sum.RelayCount)
		}
		out = append(out, row)
	}
	return out, nil
}

// dominantLink 把采样里的 p2p/relay 计数归成一个主导链路；相等时记 mixed。
func dominantLink(p2p, relay int) string {
	switch {
	case p2p == 0 && relay == 0:
		return ""
	case p2p > relay:
		return "p2p"
	case relay > p2p:
		return "relay"
	default:
		return "mixed"
	}
}

// KillSession 强制中断，必须带原因。
func (s *Service) KillSession(ctx context.Context, a Actor, sessionID, reason string) (contract.KillSessionResult, error) {
	var res contract.KillSessionResult
	if !Can(a.Role, "session.kill") {
		_ = s.audit(ctx, a, "session.kill", sessionID, "", "", "denied")
		return res, errcode.New(errcode.AdminForbidden, "session.kill")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, "session.kill", sessionID, "", "", "missing_reason")
		return res, errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		_ = s.audit(ctx, a, "session.kill", sessionID, "", "", "not_found")
		return res, err
	}
	before := sess.State
	if _, err := s.sessions.Kill(ctx, sessionID, "admin: "+reason); err != nil {
		_ = s.audit(ctx, a, "session.kill", sessionID, before, "", "error")
		return res, err
	}
	res.Closed = true

	// 先等传播结果，再写审计：审计必须记录实际发生的结果，而不是“已中断”。
	if s.closer != nil {
		if nerr := s.closer.NotifySessionClosed(sessionID, sess.ControllerID, sess.TargetID, "admin: "+reason); nerr != nil {
			res.Warning = "会话已关闭，但关闭事件传播失败：" + nerr.Error()
		} else {
			res.Propagated = true
		}
	} else {
		res.Propagated = true
	}

	result := "ok"
	if !res.Propagated {
		result = "partial: propagate_failed"
	}
	if aerr := s.audit(ctx, a, "session.kill", sessionID, before, "closed | "+reason, result); aerr != nil {
		res.Audited = false
		if res.Warning == "" {
			res.Warning = "会话已关闭，但审计写入失败"
		}
	} else {
		res.Audited = true
	}
	return res, nil
}

// Audit 返回审计日志。
func (s *Service) Audit(ctx context.Context, a Actor, page store.Page) ([]*store.AuditLog, error) {
	if !Can(a.Role, "audit.read") {
		return nil, errcode.New(errcode.AdminForbidden, "audit.read")
	}
	return s.store.ListAuditPage(ctx, page)
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

// CountUsers/CountDevices/CountSessions/CountAudit 返回与列表同过滤条件的总数。
func (s *Service) CountUsers(ctx context.Context, a Actor, page store.Page) (int, error) {
	if !Can(a.Role, "user.read") {
		return 0, errcode.New(errcode.AdminForbidden, "user.read")
	}
	return s.store.CountAccounts(ctx, page)
}

func (s *Service) CountDevices(ctx context.Context, a Actor, page store.Page) (int, error) {
	if !Can(a.Role, "device.read") {
		return 0, errcode.New(errcode.AdminForbidden, "device.read")
	}
	return s.store.CountDevices(ctx, page)
}

func (s *Service) CountSessions(ctx context.Context, a Actor, state string, page store.Page) (int, error) {
	if !Can(a.Role, "session.read") {
		return 0, errcode.New(errcode.AdminForbidden, "session.read")
	}
	return s.store.CountSessions(ctx, state, page)
}

func (s *Service) CountAudit(ctx context.Context, a Actor, page store.Page) (int, error) {
	if !Can(a.Role, "audit.read") {
		return 0, errcode.New(errcode.AdminForbidden, "audit.read")
	}
	return s.store.CountAudit(ctx, page)
}

// ForceOffline 强制下线：关闭该设备参与的未结束会话，并把设备标记为离线。
// 关闭/传播结果写进审计的 after 字段，不把"部分失败"记成成功（ADM-API-04b）。
func (s *Service) ForceOffline(ctx context.Context, a Actor, deviceID, reason string) error {
	if !Can(a.Role, "device.force_offline") {
		_ = s.audit(ctx, a, "device.force_offline", deviceID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "device.force_offline")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, "device.force_offline", deviceID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		_ = s.audit(ctx, a, "device.force_offline", deviceID, "", "", "not_found")
		return errcode.New(errcode.ConnDeviceNotFound, "device not found")
	}

	closed, notifyFailed := 0, 0
	for _, state := range []string{session.StateRequested, session.StateAuthorized, session.StateActive} {
		list, err := s.sessions.List(ctx, state)
		if err != nil {
			_ = s.audit(ctx, a, "device.force_offline", deviceID, "online", "", "error")
			return errcode.New(errcode.SrvInternal, err.Error())
		}
		for _, sess := range list {
			if sess.ControllerID != d.ID && sess.TargetID != d.ID {
				continue
			}
			if _, err := s.sessions.Kill(ctx, sess.ID, "admin force-offline: "+reason); err != nil {
				continue
			}
			closed++
			if s.closer != nil {
				if nerr := s.closer.NotifySessionClosed(sess.ID, sess.ControllerID, sess.TargetID, "admin force-offline: "+reason); nerr != nil {
					notifyFailed++
				}
			}
		}
	}
	if err := s.store.UpdateDeviceSeen(ctx, d.ID, d.LastSeenAt, false); err != nil {
		_ = s.audit(ctx, a, "device.force_offline", deviceID, "online", "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	after := fmt.Sprintf("offline; closed_sessions=%d; notify_failed=%d", closed, notifyFailed)
	return s.audit(ctx, a, "device.force_offline", d.ID, "online", after, "ok")
}

// UnbindDevice 解绑设备（删除设备行）。破坏性操作：仅超级管理员，必须填原因。
func (s *Service) UnbindDevice(ctx context.Context, a Actor, deviceID, reason string) error {
	if !Can(a.Role, "device.unbind") {
		_ = s.audit(ctx, a, "device.unbind", deviceID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "device.unbind")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, "device.unbind", deviceID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		_ = s.audit(ctx, a, "device.unbind", deviceID, "", "", "not_found")
		return errcode.New(errcode.ConnDeviceNotFound, "device not found")
	}
	if err := s.store.DeleteDevice(ctx, d.ID); err != nil {
		_ = s.audit(ctx, a, "device.unbind", d.ID, d.DeviceCode, "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	return s.audit(ctx, a, "device.unbind", d.ID, d.DeviceCode, "deleted: "+reason, "ok")
}

// AuditActions 返回审计里出现过的动作集合，供筛选下拉使用（ADM-API-05a）。
func (s *Service) AuditActions(ctx context.Context, a Actor) (contract.AuditActionsResult, error) {
	if !Can(a.Role, "audit.read") {
		return contract.AuditActionsResult{}, errcode.New(errcode.AdminForbidden, "audit.read")
	}
	actions, err := s.store.ListAuditActions(ctx)
	if err != nil {
		return contract.AuditActionsResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	if actions == nil {
		actions = []string{}
	}
	return contract.AuditActionsResult{Actions: actions}, nil
}

// SessionQuality 返回会话的真实质量采样（ADM-API-02c）。
// 没有采样时 available=false，管理端据此显示"数据未提供"，而不是画 0 曲线。
func (s *Service) SessionQuality(ctx context.Context, a Actor, sessionID string) (contract.SessionQualityResult, error) {
	if !Can(a.Role, "session.read") {
		return contract.SessionQualityResult{}, errcode.New(errcode.AdminForbidden, "session.read")
	}
	// 空/未知会话返回 available=false（而不是报错）：管理端把"没有数据"和"查询失败"分开。
	if strings.TrimSpace(sessionID) == "" {
		return contract.SessionQualityResult{SessionID: sessionID, Available: false, Samples: []contract.QualitySample{}}, nil
	}
	samples, err := s.store.ListQualitySamples(ctx, sessionID, 600)
	if err != nil {
		return contract.SessionQualityResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.QualitySample, 0, len(samples))
	for _, q := range samples {
		out = append(out, contract.QualitySample{
			TS:           q.TS,
			LatencyMs:    q.LatencyMs,
			LossPermille: q.LossPermille,
			Fps:          q.Fps,
			Tier:         q.Tier,
			Relay:        q.Relay,
		})
	}
	return contract.SessionQualityResult{
		SessionID: sessionID,
		Available: len(out) > 0,
		Samples:   out,
	}, nil
}

// DeviceQuality 返回某设备最近 N 天的质量聚合（ADM-API-02d）。
// 没有采样时 available=false，A6 显示"没有采样数据"，不画 0 曲线。
func (s *Service) DeviceQuality(ctx context.Context, a Actor, deviceID string, days int) (contract.DeviceQualityResult, error) {
	if !Can(a.Role, "device.read") {
		return contract.DeviceQualityResult{}, errcode.New(errcode.AdminForbidden, "device.read")
	}
	if days <= 0 || days > 90 {
		days = 30
	}
	if strings.TrimSpace(deviceID) == "" {
		return contract.DeviceQualityResult{DeviceID: deviceID, Available: false, Days: []contract.DeviceQualityDay{}}, nil
	}
	from := s.now().AddDate(0, 0, -days)
	rows, err := s.store.AggregateDeviceQuality(ctx, deviceID, from)
	if err != nil {
		return contract.DeviceQualityResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.DeviceQualityDay, 0, len(rows))
	for _, d := range rows {
		out = append(out, contract.DeviceQualityDay{
			Date:            d.Date,
			AvgLatencyMs:    d.AvgLatencyMs,
			AvgLossPermille: d.AvgLossPermille,
			RelayPercent:    d.RelayPercent,
			Samples:         d.Samples,
		})
	}
	return contract.DeviceQualityResult{DeviceID: deviceID, Available: len(out) > 0, Days: out}, nil
}

// OverviewSince 计算 [from, now] 窗口内的概览；质量指标来自真实采样（ADM-API-02e）。
func (s *Service) OverviewSince(ctx context.Context, a Actor, from time.Time) (contract.Overview, error) {
	if !Can(a.Role, "stats.read") {
		return contract.Overview{}, errcode.New(errcode.AdminForbidden, "stats.read")
	}
	all, err := s.collectDevices(ctx)
	if err != nil {
		return contract.Overview{}, err
	}
	devices := len(all)
	online := 0
	onlineAccounts := make(map[string]struct{})
	for _, d := range all {
		if s.online(ctx, d) {
			online++
			onlineAccounts[d.AccountID] = struct{}{}
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
	closedList, err := s.sessions.List(ctx, session.StateClosed)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	closed, abnormal := 0, 0
	for _, sess := range closedList {
		if sess.EndedAt.Before(from) {
			continue
		}
		closed++
		if session.IsAbnormal(sess.Reason) {
			abnormal++
		}
	}
	rate := 0
	if closed > 0 {
		rate = int(float64(abnormal)/float64(closed)*100 + 0.5)
	}
	attempts, activeAttempts, err := s.store.ConnectAttemptStats(ctx, from)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	connectRate := 0
	if attempts > 0 {
		connectRate = int(float64(activeAttempts)/float64(attempts)*100 + 0.5)
	}
	devicesNew, derr := s.store.CountDevicesSince(ctx, s.now().Add(-24*time.Hour))
	if derr != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, derr.Error())
	}
	health, err := s.store.HealthStatsSince(ctx, from)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	// 崩溃率的分母是"窗口内开始的会话数"：用趋势桶求和，避免再写一套计数查询。
	buckets, berr := s.store.TrendBuckets(ctx, from, time.Hour)
	sessionsStarted := 0
	if berr == nil {
		for _, b := range buckets {
			sessionsStarted += b.SessionsStarted
		}
	}
	crashRate, reconnectRate := 0, 0
	if sessionsStarted > 0 {
		crashRate = int(float64(health.Crashes)/float64(sessionsStarted)*100 + 0.5)
	}
	if health.ReconnectAttempts > 0 {
		reconnectRate = int(float64(health.ReconnectSuccess)/float64(health.ReconnectAttempts)*100 + 0.5)
	}
	linkStats, err := s.store.QualityLinkStats(ctx, from)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	p2pRate, fpsRate := 0, 0
	linkTotal := linkStats.P2P + linkStats.Relay
	if linkTotal > 0 {
		p2pRate = int(float64(linkStats.P2P)/float64(linkTotal)*100 + 0.5)
	}
	if linkStats.FpsTotal > 0 {
		fpsRate = int(float64(linkStats.FpsMet)/float64(linkStats.FpsTotal)*100 + 0.5)
	}
	q, err := s.store.QualityStatsSince(ctx, from)
	if err != nil {
		return contract.Overview{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	return contract.Overview{
		DevicesTotal:         devices,
		DevicesOnline:        online,
		UsersOnline:          len(onlineAccounts),
		SessionsActive:       len(activeList),
		SessionsPending:      len(pendingList),
		QualityAvailable:     q.Samples > 0,
		LatencyP50Ms:         q.LatencyP50Ms,
		LatencyP95Ms:         q.LatencyP95Ms,
		RelayPercent:         q.RelayPercent,
		QualitySamples:       q.Samples,
		StabilityAvailable:   closed > 0,
		ClosedSessions:       closed,
		AbnormalCloses:       abnormal,
		AbnormalCloseRate:    rate,
		ConnectAvailable:     attempts > 0,
		ConnectAttempts:      attempts,
		ConnectSuccessRate:   connectRate,
		LinkAvailable:        linkTotal > 0,
		P2pHitRate:           p2pRate,
		FpsAvailable:         linkStats.FpsTotal > 0,
		FpsTargetRate:        fpsRate,
		DevicesNewAvailable:  true,
		DevicesNew24h:        devicesNew,
		CrashAvailable:       sessionsStarted > 0,
		CrashRate:            crashRate,
		ReconnectAvailable:   health.ReconnectAttempts > 0,
		ReconnectSuccessRate: reconnectRate,
		GeneratedAt:          s.now(),
	}, nil
}

// FlagDeviceRisk 标记风险设备（ADM-API-04c）：原因必填并写审计。
func (s *Service) FlagDeviceRisk(ctx context.Context, a Actor, deviceID, reason string) error {
	return s.setDeviceRisk(ctx, a, deviceID, reason, true)
}

// ClearDeviceRisk 解除风险标记；同样要求原因，避免"悄悄解除"。
func (s *Service) ClearDeviceRisk(ctx context.Context, a Actor, deviceID, reason string) error {
	return s.setDeviceRisk(ctx, a, deviceID, reason, false)
}

func (s *Service) setDeviceRisk(ctx context.Context, a Actor, deviceID, reason string, flag bool) error {
	action := "device.clear_risk"
	if flag {
		action = "device.flag_risk"
	}
	if !Can(a.Role, action) {
		_ = s.audit(ctx, a, action, deviceID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, action)
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, deviceID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	d, err := s.store.GetDevice(ctx, deviceID)
	if err != nil {
		_ = s.audit(ctx, a, action, deviceID, "", "", "not_found")
		return errcode.New(errcode.ConnDeviceNotFound, "device not found")
	}
	before := "risk=false"
	if d.RiskFlag {
		before = "risk=true:" + d.RiskReason
	}
	after := "risk=false"
	if flag {
		after = "risk=true:" + reason
	}
	if err := s.store.SetDeviceRisk(ctx, d.ID, flag, reason); err != nil {
		_ = s.audit(ctx, a, action, d.ID, before, "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	return s.audit(ctx, a, action, d.ID, before, after, "ok")
}

// ListAdmins 返回管理员列表（ADM-API-06a）。不返回密码哈希与 TOTP 密钥。
func (s *Service) ListAdmins(ctx context.Context, a Actor, page store.Page) ([]contract.AdminRow, error) {
	if !Can(a.Role, "admin.read") {
		return nil, errcode.New(errcode.AdminForbidden, "admin.read")
	}
	admins, err := s.store.ListAdmins(ctx, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.AdminRow, 0, len(admins))
	for _, u := range admins {
		out = append(out, contract.AdminRow{
			ID:         u.ID,
			Username:   u.Username,
			Role:       u.Role,
			Status:     u.Status,
			MfaEnabled: u.TOTPSecret != "",
		})
	}
	return out, nil
}

// CountAdmins 返回管理员总数（分页用）。
func (s *Service) CountAdmins(ctx context.Context, a Actor) (int, error) {
	if !Can(a.Role, "admin.read") {
		return 0, errcode.New(errcode.AdminForbidden, "admin.read")
	}
	n, err := s.store.CountAdmins(ctx)
	if err != nil {
		return 0, errcode.New(errcode.SrvInternal, err.Error())
	}
	return n, nil
}

var validAdminRoles = map[string]bool{
	RoleSuper: true, RoleOps: true, RoleCS: true, RoleRisk: true, RoleReadonly: true,
}

// UpdateAdmin 改角色或启用/禁用管理员。
//
// 两条硬约束（防止把自己锁在门外）：
//  1. 不能修改自己；
//  2. 不能让系统失去最后一个 active 的超管。
func (s *Service) UpdateAdmin(ctx context.Context, a Actor, adminID, role, status, reason string) error {
	const action = "admin.update"
	if !Can(a.Role, "admin.write") {
		_ = s.audit(ctx, a, action, adminID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "admin.write")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, adminID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	if role != "" && !validAdminRoles[role] {
		_ = s.audit(ctx, a, action, adminID, "", "", "invalid_role")
		return errcode.New(errcode.AdminConfigInvalid, "unknown role: "+role)
	}
	if status != "" && status != "active" && status != "disabled" {
		_ = s.audit(ctx, a, action, adminID, "", "", "invalid_status")
		return errcode.New(errcode.AdminConfigInvalid, "unknown status: "+status)
	}
	target, err := s.store.GetAdmin(ctx, adminID)
	if err != nil {
		_ = s.audit(ctx, a, action, adminID, "", "", "not_found")
		return errcode.New(errcode.AdminForbidden, "admin not found: "+adminID)
	}
	// 唯一约束：任何会移除「active 超管」的操作，都必须保证系统里还剩另一个 active 超管。
	// 这条同时覆盖"禁用自己"与"降级最后一个超管"，比一刀切禁止改自己更精确。
	if target.Role == RoleSuper && target.Status == "active" && ((role != "" && role != RoleSuper) || status == "disabled") {
		all, err := s.store.ListAdmins(ctx, store.Page{Limit: 500})
		if err != nil {
			return errcode.New(errcode.SrvInternal, err.Error())
		}
		others := 0
		for _, u := range all {
			if u.ID != target.ID && u.Role == RoleSuper && u.Status == "active" {
				others++
			}
		}
		if others == 0 {
			_ = s.audit(ctx, a, action, adminID, target.Role+"/"+target.Status, "", "last_super_protected")
			return errcode.New(errcode.AdminForbidden, "cannot remove last active super admin")
		}
	}

	before := target.Role + "/" + target.Status
	if role != "" {
		if err := s.store.UpdateAdminRole(ctx, target.ID, role); err != nil {
			_ = s.audit(ctx, a, action, target.ID, before, "", "error")
			return errcode.New(errcode.SrvInternal, err.Error())
		}
	}
	if status != "" {
		if err := s.store.UpdateAdminStatus(ctx, target.ID, status); err != nil {
			_ = s.audit(ctx, a, action, target.ID, before, "", "error")
			return errcode.New(errcode.SrvInternal, err.Error())
		}
	}
	after := target.Role + "/" + target.Status
	if role != "" {
		after = role + "/" + target.Status
	}
	if status != "" {
		after = strings.Split(after, "/")[0] + "/" + status
	}
	return s.audit(ctx, a, action, target.ID, before, after, "ok")
}

var validPlatforms = map[string]bool{"windows": true, "macos": true, "android": true, "ios": true}

// ListVersions 返回客户端版本列表（A11）。
func (s *Service) ListVersions(ctx context.Context, a Actor, page store.Page) ([]contract.ClientVersionRow, error) {
	if !Can(a.Role, "version.read") {
		return nil, errcode.New(errcode.AdminForbidden, "version.read")
	}
	rows, err := s.store.ListVersions(ctx, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.ClientVersionRow, 0, len(rows))
	for _, v := range rows {
		out = append(out, contract.ClientVersionRow{
			ID:             v.ID,
			Platform:       v.Platform,
			Version:        v.Version,
			Status:         v.Status,
			RolloutPercent: v.RolloutPercent,
			ForceUpdate:    v.ForceUpdate,
			Notes:          v.Notes,
			CreatedAt:      v.CreatedAt,
			UpdatedAt:      v.UpdatedAt,
		})
	}
	return out, nil
}

// CountVersions 返回版本总数（分页用）。
func (s *Service) CountVersions(ctx context.Context, a Actor) (int, error) {
	if !Can(a.Role, "version.read") {
		return 0, errcode.New(errcode.AdminForbidden, "version.read")
	}
	n, err := s.store.CountVersions(ctx)
	if err != nil {
		return 0, errcode.New(errcode.SrvInternal, err.Error())
	}
	return n, nil
}

// validateRollout 保证「状态」与「覆盖率」一致，避免出现 full 但只有 20% 的矛盾数据。
func validateRollout(status string, percent int) string {
	switch status {
	case "draft":
		if percent != 0 {
			return "draft 版本覆盖率必须为 0"
		}
	case "rolling":
		if percent < 1 || percent > 99 {
			return "rolling 版本覆盖率必须在 1-99"
		}
	case "full":
		if percent != 100 {
			return "full 版本覆盖率必须为 100"
		}
	case "retired":
		return "retired 请使用 retire 接口"
	default:
		return "未知状态: " + status
	}
	return ""
}

// PublishVersion 发布/更新一个客户端版本（同 platform+version 幂等覆盖策略字段）。
func (s *Service) PublishVersion(ctx context.Context, a Actor, req contract.VersionPublishRequest) (contract.ClientVersionRow, error) {
	const action = "version.publish"
	if !Can(a.Role, "version.write") {
		_ = s.audit(ctx, a, action, req.Platform+"/"+req.Version, "", "", "denied")
		return contract.ClientVersionRow{}, errcode.New(errcode.AdminForbidden, "version.write")
	}
	if !validPlatforms[req.Platform] {
		_ = s.audit(ctx, a, action, req.Platform+"/"+req.Version, "", "", "invalid_platform")
		return contract.ClientVersionRow{}, errcode.New(errcode.AdminConfigInvalid, "unknown platform: "+req.Platform)
	}
	if strings.TrimSpace(req.Version) == "" {
		_ = s.audit(ctx, a, action, req.Platform+"/"+req.Version, "", "", "invalid_version")
		return contract.ClientVersionRow{}, errcode.New(errcode.AdminConfigInvalid, "empty version")
	}
	if msg := validateRollout(req.Status, req.RolloutPercent); msg != "" {
		_ = s.audit(ctx, a, action, req.Platform+"/"+req.Version, "", "", "invalid_rollout")
		return contract.ClientVersionRow{}, errcode.New(errcode.AdminConfigInvalid, msg)
	}
	v := &store.ClientVersion{
		Platform:       req.Platform,
		Version:        req.Version,
		Status:         req.Status,
		RolloutPercent: req.RolloutPercent,
		ForceUpdate:    req.ForceUpdate,
		Notes:          req.Notes,
	}
	if err := s.store.UpsertVersion(ctx, v); err != nil {
		_ = s.audit(ctx, a, action, req.Platform+"/"+req.Version, "", "", "error")
		return contract.ClientVersionRow{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	after := req.Status + "@" + fmt.Sprint(req.RolloutPercent) + "%"
	if err := s.audit(ctx, a, action, v.ID, "", after, "ok"); err != nil {
		return contract.ClientVersionRow{}, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	saved, err := s.store.GetVersion(ctx, v.ID)
	if err != nil {
		return contract.ClientVersionRow{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	return contract.ClientVersionRow{
		ID: saved.ID, Platform: saved.Platform, Version: saved.Version, Status: saved.Status,
		RolloutPercent: saved.RolloutPercent, ForceUpdate: saved.ForceUpdate, Notes: saved.Notes,
		CreatedAt: saved.CreatedAt, UpdatedAt: saved.UpdatedAt,
	}, nil
}

// RetireVersion 下线一个版本；必须填原因。
func (s *Service) RetireVersion(ctx context.Context, a Actor, versionID, reason string) error {
	const action = "version.retire"
	if !Can(a.Role, "version.write") {
		_ = s.audit(ctx, a, action, versionID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "version.write")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, versionID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	v, err := s.store.GetVersion(ctx, versionID)
	if err != nil {
		_ = s.audit(ctx, a, action, versionID, "", "", "not_found")
		return errcode.New(errcode.AdminConfigInvalid, "version not found")
	}
	if err := s.store.UpdateVersionStatus(ctx, v.ID, "retired"); err != nil {
		_ = s.audit(ctx, a, action, v.ID, v.Status, "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	return s.audit(ctx, a, action, v.ID, v.Status, "retired: "+reason, "ok")
}

// ListBlacklist 返回黑名单（A10）。
func (s *Service) ListBlacklist(ctx context.Context, a Actor, page store.Page) ([]contract.BlacklistRow, error) {
	if !Can(a.Role, "risk.read") {
		return nil, errcode.New(errcode.AdminForbidden, "risk.read")
	}
	rows, err := s.store.ListBlacklist(ctx, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.BlacklistRow, 0, len(rows))
	for _, e := range rows {
		out = append(out, toBlacklistRow(e))
	}
	return out, nil
}

func (s *Service) CountBlacklist(ctx context.Context, a Actor) (int, error) {
	if !Can(a.Role, "risk.read") {
		return 0, errcode.New(errcode.AdminForbidden, "risk.read")
	}
	n, err := s.store.CountBlacklist(ctx)
	if err != nil {
		return 0, errcode.New(errcode.SrvInternal, err.Error())
	}
	return n, nil
}

func toBlacklistRow(e *store.BlacklistEntry) contract.BlacklistRow {
	row := contract.BlacklistRow{
		ID: e.ID, Kind: e.Kind, Value: e.Value, Note: e.Note, CreatedAt: e.CreatedAt, CreatedBy: e.CreatedBy,
	}
	if e.ExpiresAt != nil {
		row.ExpiresAt = *e.ExpiresAt
	}
	return row
}

// validateBlacklistValue 只接受能被真实匹配的值：ip 支持单 IP 与 CIDR，其余要求非空。
func validateBlacklistValue(kind, value string) string {
	if strings.TrimSpace(value) == "" {
		return "empty value"
	}
	switch kind {
	case "ip":
		if strings.Contains(value, "/") {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return "invalid CIDR: " + value
			}
			return ""
		}
		if net.ParseIP(value) == nil {
			return "invalid IP: " + value
		}
	case "device", "account":
		return ""
	default:
		return "unknown kind: " + kind
	}
	return ""
}

// AddBlacklist 加入黑名单；ttl_hours 为 0/空表示永久。
func (s *Service) AddBlacklist(ctx context.Context, a Actor, req contract.BlacklistAddRequest) (contract.BlacklistRow, error) {
	const action = "risk.blacklist_add"
	if !Can(a.Role, "risk.write") {
		_ = s.audit(ctx, a, action, req.Kind+":"+req.Value, "", "", "denied")
		return contract.BlacklistRow{}, errcode.New(errcode.AdminForbidden, "risk.write")
	}
	if msg := validateBlacklistValue(req.Kind, req.Value); msg != "" {
		_ = s.audit(ctx, a, action, req.Kind+":"+req.Value, "", "", "invalid")
		return contract.BlacklistRow{}, errcode.New(errcode.AdminConfigInvalid, msg)
	}
	// 契约里 ttl_hours 是可选 int，生成后为 0 值；0 表示永久。
	ttl := req.TtlHours
	if ttl < 0 || ttl > 8760 {
		_ = s.audit(ctx, a, action, req.Kind+":"+req.Value, "", "", "invalid_ttl")
		return contract.BlacklistRow{}, errcode.New(errcode.AdminConfigInvalid, "ttl_hours must be 0-8760")
	}
	e := &store.BlacklistEntry{Kind: req.Kind, Value: req.Value, Note: req.Note, CreatedBy: a.Username}
	if ttl > 0 {
		exp := s.now().Add(time.Duration(ttl) * time.Hour)
		e.ExpiresAt = &exp
	}
	if err := s.store.AddBlacklist(ctx, e); err != nil {
		if errors.Is(err, store.ErrConflict) {
			_ = s.audit(ctx, a, action, req.Kind+":"+req.Value, "", "", "duplicate")
			return contract.BlacklistRow{}, errcode.New(errcode.AdminConfigInvalid, "already blacklisted")
		}
		_ = s.audit(ctx, a, action, req.Kind+":"+req.Value, "", "", "error")
		return contract.BlacklistRow{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	if err := s.audit(ctx, a, action, e.ID, "", req.Kind+":"+req.Value, "ok"); err != nil {
		return contract.BlacklistRow{}, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	saved, err := s.store.GetBlacklist(ctx, e.ID)
	if err != nil {
		return contract.BlacklistRow{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	return toBlacklistRow(saved), nil
}

// RemoveBlacklist 移除黑名单；必须填原因。
func (s *Service) RemoveBlacklist(ctx context.Context, a Actor, id, reason string) error {
	const action = "risk.blacklist_remove"
	if !Can(a.Role, "risk.write") {
		_ = s.audit(ctx, a, action, id, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "risk.write")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, id, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	e, err := s.store.GetBlacklist(ctx, id)
	if err != nil {
		_ = s.audit(ctx, a, action, id, "", "", "not_found")
		return errcode.New(errcode.AdminConfigInvalid, "blacklist entry not found")
	}
	if err := s.store.RemoveBlacklist(ctx, e.ID); err != nil {
		_ = s.audit(ctx, a, action, e.ID, "", "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	return s.audit(ctx, a, action, e.ID, e.Kind+":"+e.Value, "removed: "+reason, "ok")
}

// ListAlerts 返回异常连接告警（A10）。
func (s *Service) ListAlerts(ctx context.Context, a Actor, page store.Page) ([]contract.RiskAlertRow, error) {
	if !Can(a.Role, "risk.read") {
		return nil, errcode.New(errcode.AdminForbidden, "risk.read")
	}
	rows, err := s.store.ListAlerts(ctx, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.RiskAlertRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.RiskAlertRow{ID: r.ID, Kind: r.Kind, Subject: r.Subject, Detail: r.Detail, TS: r.TS})
	}
	return out, nil
}

func (s *Service) CountAlerts(ctx context.Context, a Actor) (int, error) {
	if !Can(a.Role, "risk.read") {
		return 0, errcode.New(errcode.AdminForbidden, "risk.read")
	}
	n, err := s.store.CountAlerts(ctx)
	if err != nil {
		return 0, errcode.New(errcode.SrvInternal, err.Error())
	}
	return n, nil
}

// maskIP 脱敏：IPv4 保留前两段，IPv6 保留首段；用于导出文件，避免整包外泄来源地址。
func maskIP(ip string) string {
	if ip == "" {
		return ""
	}
	// 审计里存的可能是 "IPv4:port"（RemoteAddr 原样）或裸 IPv6。
	if host, port, err := net.SplitHostPort(ip); err == nil {
		return maskIP(host) + ":" + port
	}
	if strings.Count(ip, ":") > 1 {
		// 裸 IPv6：保留首段。
		return strings.Split(ip, ":")[0] + ":****"
	}
	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		return parts[0] + "." + parts[1] + ".x.x"
	}
	return "****"
}

// csvCell 按 RFC4180 转义：含逗号/引号/换行时用双引号包裹并翻倍引号。
func csvCell(v string) string {
	if strings.ContainsAny(v, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(v, "\"", "\"\"") + "\""
	}
	return v
}

// ExportAudit 生成审计 CSV（服务端脱敏 + 转义），并记录一条 audit.export 审计。
// limit 上限 5000；命中上限时 truncated=true，前端必须提示。
func (s *Service) ExportAudit(ctx context.Context, a Actor, page store.Page, limit int) (contract.AuditExportResult, error) {
	const action = "audit.export"
	if !Can(a.Role, "audit.read") {
		_ = s.audit(ctx, a, action, "audit_logs", "", "", "denied")
		return contract.AuditExportResult{}, errcode.New(errcode.AdminForbidden, "audit.read")
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	page.Limit = limit
	page.Offset = 0
	rows, err := s.store.ListAuditPage(ctx, page)
	if err != nil {
		_ = s.audit(ctx, a, action, "audit_logs", "", "", "error")
		return contract.AuditExportResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	var b strings.Builder
	header := []string{"时间", "操作人", "角色", "动作", "对象", "变更前", "变更后", "IP", "结果"}
	for i, h := range header {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(csvCell(h))
	}
	b.WriteString("\n")
	for _, r := range rows {
		cells := []string{
			r.TS.UTC().Format(time.RFC3339),
			r.Actor, r.Role, r.Action, r.Object, r.Before, r.After, maskIP(r.IP), r.Result,
		}
		for i, c := range cells {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(csvCell(c))
		}
		b.WriteString("\n")
	}
	truncated := len(rows) >= limit
	if err := s.audit(ctx, a, action, "audit_logs", "", fmt.Sprintf("rows=%d;truncated=%v", len(rows), truncated), "ok"); err != nil {
		return contract.AuditExportResult{}, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return contract.AuditExportResult{Csv: b.String(), Rows: len(rows), Truncated: truncated}, nil
}

// killRequestTTL 是双人确认请求的有效期：过期必须重新发起，避免"几小时前的批准"被复用。
const killRequestTTL = 5 * time.Minute

// RequestKill 发起强制中断请求（第一人）；真正的中断要等第二人确认。
func (s *Service) RequestKill(ctx context.Context, a Actor, sessionID, reason string) (contract.KillRequestRow, error) {
	const action = "session.kill_request"
	if !Can(a.Role, "session.kill") {
		_ = s.audit(ctx, a, action, sessionID, "", "", "denied")
		return contract.KillRequestRow{}, errcode.New(errcode.AdminForbidden, "session.kill")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, sessionID, "", "", "missing_reason")
		return contract.KillRequestRow{}, errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		_ = s.audit(ctx, a, action, sessionID, "", "", "not_found")
		return contract.KillRequestRow{}, err
	}
	if sess.State == session.StateClosed {
		_ = s.audit(ctx, a, action, sessionID, "", "", "already_closed")
		return contract.KillRequestRow{}, errcode.New(errcode.SessionExpired, "session already closed")
	}
	now := s.now()
	req := &store.KillRequest{
		SessionID:   sess.ID,
		Reason:      strings.TrimSpace(reason),
		RequestedBy: a.Username,
		RequestedAt: now,
		ExpiresAt:   now.Add(killRequestTTL),
		Status:      "pending",
	}
	if err := s.store.CreateKillRequest(ctx, req); err != nil {
		_ = s.audit(ctx, a, action, sess.ID, "", "", "error")
		return contract.KillRequestRow{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	if err := s.audit(ctx, a, action, req.ID, "", "session="+sess.ID+";ttl=5m", "ok"); err != nil {
		return contract.KillRequestRow{}, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return toKillRequestRow(req), nil
}

func toKillRequestRow(r *store.KillRequest) contract.KillRequestRow {
	row := contract.KillRequestRow{
		ID: r.ID, SessionID: r.SessionID, Reason: r.Reason, RequestedBy: r.RequestedBy,
		RequestedAt: r.RequestedAt, ExpiresAt: r.ExpiresAt, Status: r.Status, ConfirmedBy: r.ConfirmedBy,
	}
	if !r.ConfirmedAt.IsZero() {
		row.ConfirmedAt = r.ConfirmedAt
	}
	return row
}

// ListKillRequests 列出双人确认请求（默认 pending）。
func (s *Service) ListKillRequests(ctx context.Context, a Actor, status string, page store.Page) ([]contract.KillRequestRow, error) {
	if !Can(a.Role, "session.read") {
		return nil, errcode.New(errcode.AdminForbidden, "session.read")
	}
	rows, err := s.store.ListKillRequests(ctx, status, page)
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	out := make([]contract.KillRequestRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, toKillRequestRow(r))
	}
	return out, nil
}

// ConfirmKill 由第二人确认并真正中断。
//
// 三条硬约束：
//  1. 确认人不能是发起人（否则等于单人操作）；
//  2. 只能确认 pending 且未过期的请求；
//  3. 会话若已结束，确认也要失败（不能"确认一个已经没了的东西"）。
func (s *Service) ConfirmKill(ctx context.Context, a Actor, requestID string) (contract.KillSessionResult, error) {
	const action = "session.kill_confirm"
	if !Can(a.Role, "session.kill") {
		_ = s.audit(ctx, a, action, requestID, "", "", "denied")
		return contract.KillSessionResult{}, errcode.New(errcode.AdminForbidden, "session.kill")
	}
	req, err := s.store.GetKillRequest(ctx, requestID)
	if err != nil {
		_ = s.audit(ctx, a, action, requestID, "", "", "not_found")
		return contract.KillSessionResult{}, errcode.New(errcode.SessionNotFound, "kill request not found")
	}
	if req.RequestedBy == a.Username {
		_ = s.audit(ctx, a, action, req.ID, "", "", "same_actor")
		return contract.KillSessionResult{}, errcode.New(errcode.AdminForbidden, "confirmer must differ from requester")
	}
	now := s.now()
	ok, err := s.store.ConfirmKillRequest(ctx, req.ID, a.Username, now)
	if err != nil {
		_ = s.audit(ctx, a, action, req.ID, "", "", "error")
		return contract.KillSessionResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	if !ok {
		_ = s.audit(ctx, a, action, req.ID, "", "", "not_pending_or_expired")
		return contract.KillSessionResult{}, errcode.New(errcode.SessionExpired, "request not pending or expired")
	}
	updated, err := s.sessions.Kill(ctx, req.SessionID, "admin: "+req.Reason+" (confirmed by "+a.Username+")")
	if err != nil {
		_ = s.audit(ctx, a, action, req.ID, "", "", "kill_failed")
		return contract.KillSessionResult{}, err
	}
	notified := false
	if s.closer != nil {
		if nerr := s.closer.NotifySessionClosed(updated.ID, updated.ControllerID, updated.TargetID, updated.Reason); nerr == nil {
			notified = true
		}
	}
	if err := s.audit(ctx, a, "session.kill", updated.ID, updated.State, "closed;confirmed_by="+a.Username, "ok"); err != nil {
		return contract.KillSessionResult{}, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	warning := ""
	if !notified {
		warning = "会话已关闭，但关闭事件未传播到双方（通知器不可用）"
	}
	return contract.KillSessionResult{
		Closed:     updated.State == session.StateClosed,
		Propagated: notified,
		Audited:    true,
		Warning:    warning,
	}, nil
}

// Trends 返回时间桶趋势（ADM-API-02f3）。bucket 只能是 hour/day，range 决定窗口与默认桶宽。
func (s *Service) Trends(ctx context.Context, a Actor, rangeKey, bucketKey string) (contract.TrendsResult, error) {
	if !Can(a.Role, "stats.read") {
		return contract.TrendsResult{}, errcode.New(errcode.AdminForbidden, "stats.read")
	}
	window := 24 * time.Hour
	switch rangeKey {
	case "", "24h":
		window, rangeKey = 24*time.Hour, "24h"
	case "7d":
		window = 7 * 24 * time.Hour
	case "30d":
		window = 30 * 24 * time.Hour
	default:
		return contract.TrendsResult{}, errcode.New(errcode.AdminConfigInvalid, "unknown range: "+rangeKey)
	}
	bucketDur := time.Hour
	switch bucketKey {
	case "", "hour":
		bucketKey, bucketDur = "hour", time.Hour
	case "day":
		bucketDur = 24 * time.Hour
	default:
		return contract.TrendsResult{}, errcode.New(errcode.AdminConfigInvalid, "unknown bucket: "+bucketKey)
	}
	from := s.now().Add(-window)
	rows, err := s.store.TrendBuckets(ctx, from, bucketDur)
	if err != nil {
		return contract.TrendsResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	points := make([]contract.TrendPoint, 0, len(rows))
	for _, p := range rows {
		points = append(points, contract.TrendPoint{
			BucketStart:     p.BucketStart,
			SessionsStarted: p.SessionsStarted,
			ConnectAttempts: p.ConnectAttempts,
			ConnectActive:   p.ConnectActive,
		})
	}
	return contract.TrendsResult{Range: rangeKey, Bucket: bucketKey, Points: points}, nil
}

// ListUserSessions 返回账号名下设备参与的会话（ADM-API-03c）。
func (s *Service) ListUserSessions(ctx context.Context, a Actor, accountID string, limit int) ([]contract.SessionRow, error) {
	if !Can(a.Role, "user.read") {
		return nil, errcode.New(errcode.AdminForbidden, "user.read")
	}
	if strings.TrimSpace(accountID) == "" {
		return []contract.SessionRow{}, nil
	}
	list, err := s.store.ListSessionsByAccount(ctx, accountID, limit)
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

// GetUserNote 读账号备注；没写过时返回空串（不是 404，前端少一个分支）。
func (s *Service) GetUserNote(ctx context.Context, a Actor, accountID string) (contract.AccountNoteResult, error) {
	if !Can(a.Role, "user.read") {
		return contract.AccountNoteResult{}, errcode.New(errcode.AdminForbidden, "user.read")
	}
	n, err := s.store.GetAccountNote(ctx, accountID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return contract.AccountNoteResult{AccountID: accountID}, nil
		}
		return contract.AccountNoteResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	row := contract.AccountNoteResult{
		AccountID: n.AccountID, Note: n.Note, UpdatedBy: n.UpdatedBy,
	}
	if !n.UpdatedAt.IsZero() {
		row.UpdatedAt = n.UpdatedAt
	}
	return row, nil
}

// SetUserNote 写账号备注；备注属于运营判断，写操作进审计（before/after）。
func (s *Service) SetUserNote(ctx context.Context, a Actor, accountID, note string) error {
	const action = "user.note"
	if !Can(a.Role, "user.write") {
		_ = s.audit(ctx, a, action, accountID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "user.write")
	}
	if strings.TrimSpace(accountID) == "" {
		_ = s.audit(ctx, a, action, accountID, "", "", "invalid")
		return errcode.New(errcode.AdminConfigInvalid, "empty account_id")
	}
	before := ""
	if prev, err := s.store.GetAccountNote(ctx, accountID); err == nil {
		before = prev.Note
	}
	trimmed := strings.TrimSpace(note)
	if err := s.store.UpsertAccountNote(ctx, &store.AccountNote{
		AccountID: accountID, Note: trimmed, UpdatedBy: a.Username,
	}); err != nil {
		_ = s.audit(ctx, a, action, accountID, before, "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	return s.audit(ctx, a, action, accountID, before, trimmed, "ok")
}

// UnbanUser 解除封禁（A3 / ADM-API-03d）。必须填原因并写审计。
func (s *Service) UnbanUser(ctx context.Context, a Actor, accountID, reason string) error {
	const action = "user.unban"
	if !Can(a.Role, "user.ban") {
		_ = s.audit(ctx, a, action, accountID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "user.ban")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, accountID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	acc, err := s.store.GetAccount(ctx, accountID)
	if err != nil {
		_ = s.audit(ctx, a, action, accountID, "", "", "not_found")
		return errcode.New(errcode.AdminForbidden, "account not found")
	}
	if acc.Status == "active" {
		_ = s.audit(ctx, a, action, accountID, acc.Status, "", "noop")
		return nil
	}
	before := acc.Status
	if err := s.store.UpdateAccountStatus(ctx, accountID, "active"); err != nil {
		_ = s.audit(ctx, a, action, accountID, before, "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	return s.audit(ctx, a, action, accountID, before, "active | "+reason, "ok")
}

// KickUser 踢下线：关闭该账号所有设备参与的未结束会话，并把设备标记离线。
// 与"强制下线单台设备"共用同一套会话关闭语义，区别只是作用域是整个账号。
func (s *Service) KickUser(ctx context.Context, a Actor, accountID, reason string) error {
	const action = "user.kick"
	if !Can(a.Role, "user.ban") {
		_ = s.audit(ctx, a, action, accountID, "", "", "denied")
		return errcode.New(errcode.AdminForbidden, "user.ban")
	}
	if strings.TrimSpace(reason) == "" {
		_ = s.audit(ctx, a, action, accountID, "", "", "missing_reason")
		return errcode.New(errcode.AdminReasonRequired, "reason required")
	}
	devices, err := s.store.ListDevicesByAccount(ctx, accountID, store.Page{Limit: 200})
	if err != nil {
		_ = s.audit(ctx, a, action, accountID, "", "", "error")
		return errcode.New(errcode.SrvInternal, err.Error())
	}
	owned := map[string]*store.Device{}
	for _, d := range devices {
		owned[d.ID] = d
	}
	closed, notifyFailed, offline := 0, 0, 0
	for _, state := range []string{session.StateRequested, session.StateAuthorized, session.StateActive} {
		list, lerr := s.sessions.List(ctx, state)
		if lerr != nil {
			_ = s.audit(ctx, a, action, accountID, "", "", "error")
			return errcode.New(errcode.SrvInternal, lerr.Error())
		}
		for _, sess := range list {
			_, cOK := owned[sess.ControllerID]
			_, tOK := owned[sess.TargetID]
			if !cOK && !tOK {
				continue
			}
			if _, kerr := s.sessions.Kill(ctx, sess.ID, "admin kick: "+reason); kerr != nil {
				continue
			}
			closed++
			if s.closer != nil {
				if nerr := s.closer.NotifySessionClosed(sess.ID, sess.ControllerID, sess.TargetID, "admin kick: "+reason); nerr != nil {
					notifyFailed++
				}
			}
		}
	}
	for _, d := range devices {
		if !d.Online {
			continue
		}
		if uerr := s.store.UpdateDeviceSeen(ctx, d.ID, d.LastSeenAt, false); uerr == nil {
			offline++
		}
	}
	after := fmt.Sprintf("closed_sessions=%d;notify_failed=%d;devices_offline=%d", closed, notifyFailed, offline)
	return s.audit(ctx, a, action, accountID, "", after+" | "+reason, "ok")
}

// ExportUsers 生成用户列表 CSV（ADM-API-03e）：服务端脱敏（手机号用 phone_mask）+ 导出审计。
func (s *Service) ExportUsers(ctx context.Context, a Actor, page store.Page, limit int) (contract.CsvExportResult, error) {
	const action = "user.export"
	if !Can(a.Role, "user.read") {
		_ = s.audit(ctx, a, action, "accounts", "", "", "denied")
		return contract.CsvExportResult{}, errcode.New(errcode.AdminForbidden, "user.read")
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	page.Limit = limit
	page.Offset = 0
	rows, err := s.store.ListAccounts(ctx, page)
	if err != nil {
		_ = s.audit(ctx, a, action, "accounts", "", "", "error")
		return contract.CsvExportResult{}, errcode.New(errcode.SrvInternal, err.Error())
	}
	ids := make([]string, 0, len(rows))
	for _, u := range rows {
		ids = append(ids, u.ID)
	}
	counts, cerr := s.store.DeviceCountsByAccount(ctx, ids)
	if cerr != nil {
		counts = map[string]int{}
	}
	var b strings.Builder
	header := []string{"账号 ID", "昵称", "手机号", "设备数", "状态", "注册时间", "最近登录"}
	for i, h := range header {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(csvCell(h))
	}
	b.WriteString("\n")
	for _, u := range rows {
		cells := []string{
			u.ID,
			u.Nickname,
			// 导出同样脱敏：手机号只出掩码，避免 CSV 外泄完整号码。
			maskPhone(u.PhoneEnc),
			fmt.Sprint(counts[u.ID]),
			u.Status,
			u.CreatedAt.UTC().Format(time.RFC3339),
			formatOptTime(u.LastLoginAt),
		}
		for i, c := range cells {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(csvCell(c))
		}
		b.WriteString("\n")
	}
	truncated := len(rows) >= limit
	if err := s.audit(ctx, a, action, "accounts", "", fmt.Sprintf("rows=%d;truncated=%v", len(rows), truncated), "ok"); err != nil {
		return contract.CsvExportResult{}, errcode.New(errcode.SrvInternal, "audit write failed")
	}
	return contract.CsvExportResult{Csv: b.String(), Rows: len(rows), Truncated: truncated}, nil
}

// formatOptTime 空时间导出为空串（避免 0001-01-01 出现在 CSV 里）。
func formatOptTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// SessionDetail 返回会话详情扩展（ADM-API-03g）：双方设备、链路切换、状态时间线。
//
// 说明：错误码时间线暂不提供——服务端没有按会话记录错误事件；这里只展示
// 真实存在的状态时间线（创建/结束）与链路切换（来自客户端采样），不臆造错误码。
func (s *Service) SessionDetail(ctx context.Context, a Actor, sessionID string) (contract.SessionDetailResult, error) {
	if !Can(a.Role, "session.read") {
		return contract.SessionDetailResult{}, errcode.New(errcode.AdminForbidden, "session.read")
	}
	// 空 session_id 返回空结构而不是错误：信封契约要求 GET+admin 恒为 200，
	// 管理端据"双方设备为空"判断会话不存在（与 ADM-17 的处理一致）。
	if strings.TrimSpace(sessionID) == "" {
		return contract.SessionDetailResult{LinkSwitches: []contract.LinkSwitch{}, Timeline: []contract.TimelineEntry{}}, nil
	}
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return contract.SessionDetailResult{}, err
	}
	party := func(deviceID string) contract.SessionParty {
		d, derr := s.store.GetDevice(ctx, deviceID)
		if derr != nil {
			return contract.SessionParty{DeviceID: deviceID}
		}
		return contract.SessionParty{
			DeviceID: d.ID, DeviceCode: d.DeviceCode, Platform: d.Platform,
			OsVersion: d.OSVersion, ClientVersion: d.ClientVersion, Online: d.Online,
		}
	}
	samples, qerr := s.store.ListQualitySamples(ctx, sess.ID, 600)
	if qerr != nil {
		return contract.SessionDetailResult{}, errcode.New(errcode.SrvInternal, qerr.Error())
	}
	switches := make([]contract.LinkSwitch, 0)
	prev := ""
	for _, sm := range samples {
		if sm.Link == "" {
			continue
		}
		if prev != "" && sm.Link != prev {
			switches = append(switches, contract.LinkSwitch{TS: sm.TS, From: prev, To: sm.Link})
		}
		prev = sm.Link
	}
	timeline := []contract.TimelineEntry{
		{TS: sess.StartedAt, Label: "创建会话", Detail: "requested"},
	}
	if !sess.EndedAt.IsZero() {
		timeline = append(timeline, contract.TimelineEntry{
			TS: sess.EndedAt, Label: "结束会话", Detail: sess.Reason,
		})
	}
	errs, eerr := s.store.ListSessionErrors(ctx, sess.ID, 100)
	if eerr != nil {
		return contract.SessionDetailResult{}, errcode.New(errcode.SrvInternal, eerr.Error())
	}
	errorEvents := make([]contract.SessionErrorRow, 0, len(errs))
	for _, e := range errs {
		errorEvents = append(errorEvents, contract.SessionErrorRow{
			TS: e.TS, Code: e.Code, Name: e.Name, Detail: e.Detail, Source: e.Source,
		})
	}
	return contract.SessionDetailResult{
		Controller:   party(sess.ControllerID),
		Target:       party(sess.TargetID),
		LinkSwitches: switches,
		Timeline:     timeline,
		ErrorEvents:  errorEvents,
	}, nil
}
