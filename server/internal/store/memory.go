package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// randomID 生成 8 字节随机十六进制 ID，避免 ID 可枚举。
func randomID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "-" + hex.EncodeToString([]byte(time.Now().UTC().Format("150405.000000")))
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// Memory 是 Store 的内存实现。用于测试与本地联调；生产用 PG。
type Memory struct {
	mu       sync.RWMutex
	accounts map[string]*Account
	byPhone  map[string]string
	devices  map[string]*Device
	byCode   map[string]string
	codes    map[string]*VerifyCode
	trust    map[string]*TrustEdge
	sessions map[string]*Session
	admins   map[string]*AdminUser
	audits   []*AuditLog
	files    map[string]*FileMeta
	assist   map[string]*AssistCode
	config   map[string]string
	// configScoped: platform -> key -> value（ADM-API-05c1）。
	configScoped map[string]map[string]string
	// quality: sessionID -> unix 秒 -> 采样（同一秒幂等覆盖，ADM-API-02c）。
	quality map[string]map[int64]QualitySample
	// versions: 客户端版本（ADM-API-06b），按 platform+version 唯一。
	versions map[string]*ClientVersion
	// sessionErrors: 会话级错误事件（ADM-API-03h）。
	sessionErrors []*SessionError
	// health: 客户端崩溃/重连事件（ADM-API-02f5）。
	health []*HealthEvent
	// notes: 账号备注（ADM-API-03c）。
	notes map[string]*AccountNote
	// connectAttempts: 建连尝试埋点（ADM-API-02f2），按 session 唯一。
	connectAttempts map[string]*ConnectAttempt
	// killRequests: 双人确认中断请求（ADM-API-07b1）。
	killRequests map[string]*KillRequest
	// blacklist / alerts: 风控（ADM-API-06c）。
	blacklist map[string]*BlacklistEntry
	alerts    []*RiskAlert
	// sessionEvents 以 sessionID|version 为键做 outbox 幂等去重。
	sessionEvents map[string]*SessionEvent
	closed        bool
}

// NewMemory 创建内存存储。
func NewMemory() *Memory {
	return &Memory{
		accounts:      map[string]*Account{},
		byPhone:       map[string]string{},
		devices:       map[string]*Device{},
		byCode:        map[string]string{},
		codes:         map[string]*VerifyCode{},
		trust:         map[string]*TrustEdge{},
		sessions:      map[string]*Session{},
		admins:        map[string]*AdminUser{},
		files:         map[string]*FileMeta{},
		assist:        map[string]*AssistCode{},
		config:        map[string]string{},
		sessionEvents: map[string]*SessionEvent{},
	}
}

var _ Store = (*Memory)(nil)

func pageBounds(p Page, def int) (int, int) {
	limit := p.Limit
	if limit <= 0 {
		limit = def
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func (m *Memory) CreateAccount(_ context.Context, a *Account) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byPhone[a.PhoneHash]; ok {
		return ErrConflict
	}
	if a.ID == "" {
		a.ID = randomID("acc")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	cp := *a
	m.accounts[a.ID] = &cp
	m.byPhone[a.PhoneHash] = a.ID
	return nil
}

func (m *Memory) GetAccountByPhoneHash(_ context.Context, hash string) (*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byPhone[hash]
	if !ok {
		return nil, ErrNotFound
	}
	a, ok := m.accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *Memory) GetAccount(_ context.Context, id string) (*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.accounts[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *Memory) UpdateAccountStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return ErrNotFound
	}
	a.Status = status
	return nil
}

func (m *Memory) UpdateAccountPhoneEnc(_ context.Context, id, phoneEnc string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return ErrNotFound
	}
	a.PhoneEnc = phoneEnc
	return nil
}

func (m *Memory) TouchAccountLogin(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[id]
	if !ok {
		return ErrNotFound
	}
	a.LastLoginAt = at
	return nil
}

func (m *Memory) ListAccounts(_ context.Context, p Page) ([]*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*Account, 0, len(m.accounts))
	q := strings.ToLower(p.Query)
	for _, a := range m.accounts {
		if p.Status != "" && a.Status != p.Status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.ID+" "+a.PhoneEnc+" "+a.Status), q) {
			continue
		}
		// 时间范围（ADM-API-03e）：按注册时间过滤。
		if !p.From.IsZero() && a.CreatedAt.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && a.CreatedAt.After(p.To) {
			continue
		}
		cp := *a
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool {
		less := all[i].CreatedAt.Before(all[j].CreatedAt)
		switch p.Sort {
		case "id":
			less = all[i].ID < all[j].ID
		case "status":
			less = all[i].Status < all[j].Status
		}
		if p.Desc {
			return !less
		}
		return less
	})
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) CreateDevice(_ context.Context, d *Device) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byCode[d.DeviceCode]; ok {
		return ErrConflict
	}
	if d.ID == "" {
		d.ID = randomID("dev")
	}
	cp := *d
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	m.devices[d.ID] = &cp
	m.byCode[d.DeviceCode] = d.ID
	return nil
}

func (m *Memory) GetDeviceByCode(_ context.Context, code string) (*Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byCode[code]
	if !ok {
		return nil, ErrNotFound
	}
	d, ok := m.devices[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *Memory) GetDevice(_ context.Context, id string) (*Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *Memory) ListDevicesByAccount(_ context.Context, accountID string, p Page) ([]*Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Device
	for _, d := range m.devices {
		if d.AccountID == accountID {
			cp := *d
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceCode < out[j].DeviceCode })
	limit, offset := pageBounds(p, 100)
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (m *Memory) DeviceCountsByAccount(_ context.Context, accountIDs []string) (map[string]int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	want := make(map[string]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		want[id] = struct{}{}
	}
	out := make(map[string]int, len(accountIDs))
	for _, d := range m.devices {
		if _, ok := want[d.AccountID]; ok {
			out[d.AccountID]++
		}
	}
	return out, nil
}

func (m *Memory) ListDevices(_ context.Context, p Page) ([]*Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*Device, 0, len(m.devices))
	q := strings.ToLower(p.Query)
	for _, d := range m.devices {
		if p.Platform != "" && d.Platform != p.Platform {
			continue
		}
		if p.Online != nil && d.Online != *p.Online {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(d.ID+" "+d.DeviceCode+" "+d.Platform+" "+d.AccountID), q) {
			continue
		}
		cp := *d
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool {
		less := all[i].DeviceCode < all[j].DeviceCode
		switch p.Sort {
		case "platform":
			less = all[i].Platform < all[j].Platform
		case "last_seen":
			less = all[i].LastSeenAt.Before(all[j].LastSeenAt)
		}
		if p.Desc {
			return !less
		}
		return less
	})
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) UpdateDeviceSeen(_ context.Context, id string, at time.Time, online bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.LastSeenAt = at
	d.Online = online
	return nil
}

func (m *Memory) UpdateDevicePlatform(_ context.Context, id, platform string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.Platform = platform
	return nil
}

func (m *Memory) UpdateDevicePassword(_ context.Context, id, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.PasswordHash = passwordHash
	return nil
}

func (m *Memory) DeleteDevice(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.devices[id]; !ok {
		return ErrNotFound
	}
	delete(m.devices, id)
	return nil
}

func (m *Memory) SetDeviceRisk(_ context.Context, id string, flag bool, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.RiskFlag = flag
	if flag {
		d.RiskReason = reason
	} else {
		d.RiskReason = ""
	}
	return nil
}

func (m *Memory) UpdateDeviceMeta(_ context.Context, id, deviceName, osVersion, clientVersion, natType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	// 只覆盖非空字段：客户端某次没上报的字段不能被清空。
	if deviceName != "" {
		d.DeviceName = deviceName
	}
	if osVersion != "" {
		d.OSVersion = osVersion
	}
	if clientVersion != "" {
		d.ClientVersion = clientVersion
	}
	if natType != "" {
		d.NATType = natType
	}
	return nil
}

func (m *Memory) UpdateDeviceToken(_ context.Context, id, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.TokenHash = tokenHash
	return nil
}

func (m *Memory) UpdateDeviceIdentity(_ context.Context, id, pubkey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.IdentityPubKey = pubkey
	return nil
}

func (m *Memory) GetDeviceIdentity(_ context.Context, id string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d, ok := m.devices[id]
	if !ok {
		return "", ErrNotFound
	}
	return d.IdentityPubKey, nil
}

func (m *Memory) UpsertVerifyCode(_ context.Context, v *VerifyCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *v
	m.codes[v.DeviceCode] = &cp
	return nil
}

func (m *Memory) GetVerifyCode(_ context.Context, deviceCode string) (*VerifyCode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.codes[deviceCode]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

// VerifyAndConsume 在单把锁内完成校验与消费，保证并发安全。
func (m *Memory) VerifyAndConsume(_ context.Context, deviceCode, codeHash string, now time.Time, maxAttempts int, lockFor time.Duration) (VerifyResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	vc, ok := m.codes[deviceCode]
	if !ok {
		return VerifyResult{Outcome: VerifyExpired}, nil
	}
	if !vc.LockedTill.IsZero() && now.Before(vc.LockedTill) {
		return VerifyResult{Outcome: VerifyLocked, Attempts: vc.Attempts, LockedTo: vc.LockedTill}, nil
	}
	if now.After(vc.ExpiresAt) {
		return VerifyResult{Outcome: VerifyExpired, Attempts: vc.Attempts}, nil
	}
	if !constantTimeEqual(vc.CodeHash, codeHash) {
		vc.Attempts++
		if maxAttempts > 0 && vc.Attempts >= maxAttempts {
			vc.LockedTill = now.Add(lockFor)
		}
		out := VerifyInvalid
		if !vc.LockedTill.IsZero() && now.Before(vc.LockedTill) {
			out = VerifyLocked
		}
		return VerifyResult{Outcome: out, Attempts: vc.Attempts, LockedTo: vc.LockedTill}, nil
	}
	vc.ExpiresAt = now.Add(-time.Second)
	return VerifyResult{Outcome: VerifyOK, Attempts: vc.Attempts}, nil
}

func (m *Memory) UpsertTrust(_ context.Context, t *TrustEdge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.trust[t.ControllerID+"->"+t.TargetID] = &cp
	return nil
}

func (m *Memory) HasTrust(_ context.Context, controllerID, targetID string, now time.Time) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.trust[controllerID+"->"+targetID]
	if !ok {
		return false, nil
	}
	return now.Before(t.ExpiresAt), nil
}

func (m *Memory) CreateSession(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == "" {
		s.ID = randomID("ses")
	}
	if s.Version == 0 {
		s.Version = 1
	}
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}

func (m *Memory) CreateSessionExclusive(_ context.Context, s *Session) (*Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cur := range m.sessions {
		if cur.ControllerID == s.ControllerID && cur.TargetID == s.TargetID && cur.State != "closed" {
			cp := *cur
			return &cp, false, nil
		}
	}
	if s.ID == "" {
		s.ID = randomID("ses")
	}
	if s.Version == 0 {
		s.Version = 1
	}
	cp := *s
	m.sessions[s.ID] = &cp
	out := *s
	return &out, true, nil
}

func (m *Memory) GetSession(_ context.Context, id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *Memory) UpdateSession(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[s.ID]; !ok {
		return ErrNotFound
	}
	if s.Version == 0 {
		s.Version = 1
	}
	s.Version++
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}

func (m *Memory) UpdateSessionCASWithEvent(ctx context.Context, s *Session, expected int64, ev *SessionEvent) error {
	if err := m.UpdateSessionCAS(ctx, s, expected); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.SessionID == "" {
		ev.SessionID = s.ID
	}
	if ev.Version == 0 {
		ev.Version = s.Version
	}
	if ev.ID == "" {
		ev.ID = randomID("evt")
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	key := fmt.Sprintf("%s|%d", ev.SessionID, ev.Version)
	if _, ok := m.sessionEvents[key]; ok {
		return nil
	}
	cp := *ev
	m.sessionEvents[key] = &cp
	return nil
}

func (m *Memory) ListSessionEvents(_ context.Context, delivered bool, limit int) ([]*SessionEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := make([]*SessionEvent, 0, limit)
	for _, ev := range m.sessionEvents {
		if ev.Delivered == delivered {
			cp := *ev
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) MarkSessionEventDelivered(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ev := range m.sessionEvents {
		if ev.ID == id {
			ev.Delivered = true
			ev.LastError = ""
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) MarkSessionEventFailed(_ context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ev := range m.sessionEvents {
		if ev.ID == id {
			ev.Attempts++
			ev.LastError = reason
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) UpdateSessionCAS(_ context.Context, s *Session, expected int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.sessions[s.ID]
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expected {
		return ErrVersionConflict
	}
	s.Version = expected + 1
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}

func (m *Memory) ListSessions(_ context.Context, state string, p Page) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Session
	q := strings.ToLower(p.Query)
	for _, s := range m.sessions {
		if state != "" && s.State != state {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.ID+" "+s.ControllerID+" "+s.TargetID+" "+s.State), q) {
			continue
		}
		// 时间范围 / 账号 / 设备（ADM-API-03f1）：全部下推到存储层。
		if !p.From.IsZero() && s.StartedAt.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && s.StartedAt.After(p.To) {
			continue
		}
		if p.DeviceID != "" && s.ControllerID != p.DeviceID && s.TargetID != p.DeviceID {
			continue
		}
		if p.AccountID != "" {
			owned := false
			for _, id := range []string{s.ControllerID, s.TargetID} {
				if d, ok := m.devices[id]; ok && d.AccountID == p.AccountID {
					owned = true
					break
				}
			}
			if !owned {
				continue
			}
		}
		if p.CloseKind != "" && s.CloseKind != p.CloseKind {
			continue
		}
		if p.Abnormal != nil {
			if s.State != "closed" || IsAbnormalCloseKind(s.CloseKind) != *p.Abnormal {
				continue
			}
		}
		if p.Link != "" && dominantSessionLink(m.quality[s.ID]) != p.Link {
			continue
		}
		cp := *s
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		less := out[i].StartedAt.Before(out[j].StartedAt)
		switch p.Sort {
		case "id":
			less = out[i].ID < out[j].ID
		case "state":
			less = out[i].State < out[j].State
		}
		if p.Desc {
			return !less
		}
		return less
	})
	limit, offset := pageBounds(p, 100)
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

// ---------- files ----------

func (m *Memory) CreateFile(_ context.Context, f *FileMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f.ID == "" {
		f.ID = randomID("fil")
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	cp := *f
	m.files[f.ID] = &cp
	return nil
}

func (m *Memory) GetFile(_ context.Context, id string) (*FileMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.files[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *f
	return &cp, nil
}

func (m *Memory) ListFilesByAccount(_ context.Context, accountID string, p Page) ([]*FileMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*FileMeta
	for _, f := range m.files {
		if f.AccountID == accountID {
			cp := *f
			out = append(out, &cp)
		}
	}
	// 按创建时间倒序；时间相同再按 ID 倒序，保证分页稳定。
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	limit, offset := pageBounds(p, 20)
	if offset >= len(out) {
		return nil, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (m *Memory) DeleteFile(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[id]; !ok {
		return ErrNotFound
	}
	delete(m.files, id)
	return nil
}

// ---------- assist codes ----------

func (m *Memory) PutAssistCode(_ context.Context, c *AssistCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	cp := *c
	m.assist[c.Code] = &cp
	return nil
}

func (m *Memory) GetAssistCode(_ context.Context, code string) (*AssistCode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.assist[code]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

// RedeemAssistCode 在单把锁内完成「检查 + 标记已兑换」，
// 因此并发兑换同一个码时只有一个成功。
func (m *Memory) RedeemAssistCode(_ context.Context, code string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.assist[code]
	if !ok {
		return false, nil
	}
	if !c.RedeemedAt.IsZero() {
		return false, nil
	}
	if !now.Before(c.ExpiresAt) {
		return false, nil
	}
	c.RedeemedAt = now
	return true, nil
}

// ---------- app config ----------

func (m *Memory) PutConfig(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config[key] = value
	return nil
}

func (m *Memory) ListConfigOverrides(_ context.Context, platform, version string) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.config
	if platform != "" || version != "" {
		src = m.configScoped[platform+"@"+version]
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out, nil
}

func (m *Memory) SetConfigOverride(_ context.Context, key, platform, version, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if platform == "" && version == "" {
		if m.config == nil {
			m.config = make(map[string]string)
		}
		m.config[key] = value
		return nil
	}
	if m.configScoped == nil {
		m.configScoped = make(map[string]map[string]string)
	}
	scope := platform + "@" + version
	if m.configScoped[scope] == nil {
		m.configScoped[scope] = make(map[string]string)
	}
	m.configScoped[scope][key] = value
	return nil
}

func (m *Memory) ListConfig(_ context.Context) (map[string]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.config))
	for k, v := range m.config {
		out[k] = v
	}
	return out, nil
}

func (m *Memory) GetAdminByUsername(_ context.Context, username string) (*AdminUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.admins {
		if a.Username == username {
			cp := *a
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) GetAdmin(_ context.Context, id string) (*AdminUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.admins[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *Memory) ListAdmins(_ context.Context, p Page) ([]*AdminUser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*AdminUser, 0, len(m.admins))
	for _, a := range m.admins {
		cp := *a
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Username < all[j].Username })
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) CountAdmins(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.admins), nil
}

func (m *Memory) UpdateAdminRole(_ context.Context, id, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.admins[id]
	if !ok {
		return ErrNotFound
	}
	a.Role = role
	return nil
}

func (m *Memory) UpdateAdminStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.admins[id]
	if !ok {
		return ErrNotFound
	}
	a.Status = status
	return nil
}

func (m *Memory) PutAdmin(_ context.Context, a *AdminUser) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = randomID("adm")
	}
	for _, existing := range m.admins {
		if existing.Username == a.Username {
			cp := *a
			cp.ID = existing.ID
			m.admins[existing.ID] = &cp
			return nil
		}
	}
	cp := *a
	m.admins[a.ID] = &cp
	return nil
}

func (m *Memory) AppendAudit(_ context.Context, a *AuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = randomID("aud")
	}
	if a.TS.IsZero() {
		a.TS = time.Now().UTC()
	}
	cp := *a
	m.audits = append(m.audits, &cp)
	return nil
}

func (m *Memory) ListAudit(_ context.Context, limit int) ([]*AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	out := make([]*AuditLog, 0, limit)
	for i := len(m.audits) - 1; i >= 0 && len(out) < limit; i-- {
		cp := *m.audits[i]
		out = append(out, &cp)
	}
	return out, nil
}

func (m *Memory) Ping(context.Context) error { return nil }

func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// constantTimeEqual 是等长度常量时间比较。
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
func (m *Memory) CountAccounts(_ context.Context, p Page) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q := strings.ToLower(p.Query)
	n := 0
	for _, a := range m.accounts {
		if p.Status != "" && a.Status != p.Status {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.ID+" "+a.PhoneEnc+" "+a.Status), q) {
			continue
		}
		if !p.From.IsZero() && a.CreatedAt.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && a.CreatedAt.After(p.To) {
			continue
		}
		n++
	}
	return n, nil
}

func (m *Memory) CountDevices(_ context.Context, p Page) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q := strings.ToLower(p.Query)
	n := 0
	for _, d := range m.devices {
		if p.Platform != "" && d.Platform != p.Platform {
			continue
		}
		if p.Online != nil && d.Online != *p.Online {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(d.ID+" "+d.DeviceCode+" "+d.Platform+" "+d.AccountID), q) {
			continue
		}
		n++
	}
	return n, nil
}

func (m *Memory) CountSessions(_ context.Context, state string, p Page) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q := strings.ToLower(p.Query)
	n := 0
	for _, s := range m.sessions {
		if state != "" && s.State != state {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.ID+" "+s.ControllerID+" "+s.TargetID+" "+s.State), q) {
			continue
		}
		if !p.From.IsZero() && s.StartedAt.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && s.StartedAt.After(p.To) {
			continue
		}
		if p.DeviceID != "" && s.ControllerID != p.DeviceID && s.TargetID != p.DeviceID {
			continue
		}
		if p.AccountID != "" {
			owned := false
			for _, id := range []string{s.ControllerID, s.TargetID} {
				if d, ok := m.devices[id]; ok && d.AccountID == p.AccountID {
					owned = true
					break
				}
			}
			if !owned {
				continue
			}
		}
		if p.CloseKind != "" && s.CloseKind != p.CloseKind {
			continue
		}
		if p.Abnormal != nil {
			if s.State != "closed" || IsAbnormalCloseKind(s.CloseKind) != *p.Abnormal {
				continue
			}
		}
		if p.Link != "" && dominantSessionLink(m.quality[s.ID]) != p.Link {
			continue
		}
		n++
	}
	return n, nil
}

func (m *Memory) ListAuditPage(_ context.Context, p Page) ([]*AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q := strings.ToLower(p.Query)
	all := make([]*AuditLog, 0)
	for i := len(m.audits) - 1; i >= 0; i-- {
		a := m.audits[i]
		if p.Action != "" && a.Action != p.Action {
			continue
		}
		if p.Result != "" && a.Result != p.Result {
			continue
		}
		if p.Actor != "" && a.Actor != p.Actor {
			continue
		}
		if p.Object != "" && a.Object != p.Object {
			continue
		}
		if !p.From.IsZero() && a.TS.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && a.TS.After(p.To) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.Actor+" "+a.Action+" "+a.Object+" "+a.Result), q) {
			continue
		}
		cp := *a
		all = append(all, &cp)
	}
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) AppendQualitySamples(_ context.Context, samples []QualitySample) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.quality == nil {
		m.quality = make(map[string]map[int64]QualitySample)
	}
	for _, s := range samples {
		if s.SessionID == "" {
			continue
		}
		if m.quality[s.SessionID] == nil {
			m.quality[s.SessionID] = make(map[int64]QualitySample)
		}
		m.quality[s.SessionID][s.TS.Unix()] = s
	}
	return nil
}

func (m *Memory) ListQualitySamples(_ context.Context, sessionID string, limit int) ([]QualitySample, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byTS := m.quality[sessionID]
	out := make([]QualitySample, 0, len(byTS))
	for _, s := range byTS {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// dominantSessionLink 与 adminapi.dominantLink 同口径：多数票，相等 mixed，无上报空。
func dominantSessionLink(bySecond map[int64]QualitySample) string {
	p2p, relay := 0, 0
	for _, s := range bySecond {
		switch s.Link {
		case "p2p":
			p2p++
		case "relay":
			relay++
		}
	}
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

func (m *Memory) SessionSummaries(_ context.Context, sessionIDs []string) (map[string]SessionSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]SessionSummary, len(sessionIDs))
	for _, id := range sessionIDs {
		bySecond := m.quality[id]
		if len(bySecond) == 0 {
			continue
		}
		sum := SessionSummary{}
		total := 0
		for _, s := range bySecond {
			sum.Samples++
			total += s.LatencyMs
			switch s.Link {
			case "p2p":
				sum.P2PCount++
			case "relay":
				sum.RelayCount++
			}
		}
		if sum.Samples > 0 {
			sum.AvgLatencyMs = total / sum.Samples
		}
		out[id] = sum
	}
	return out, nil
}

func (m *Memory) CountDevicesSince(_ context.Context, from time.Time) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, d := range m.devices {
		if d.CreatedAt.IsZero() || d.CreatedAt.Before(from) {
			continue
		}
		n++
	}
	return n, nil
}

func (m *Memory) RecordSessionError(_ context.Context, e *SessionError) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *e
	if cp.ID == "" {
		cp.ID = randomID("se")
	}
	if cp.TS.IsZero() {
		cp.TS = time.Now().UTC()
	}
	m.sessionErrors = append(m.sessionErrors, &cp)
	return nil
}

func (m *Memory) ListSessionErrors(_ context.Context, sessionID string, limit int) ([]*SessionError, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*SessionError
	for _, e := range m.sessionErrors {
		if e.SessionID == sessionID {
			cp := *e
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS.Before(out[j].TS) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (m *Memory) RecordHealthEvent(_ context.Context, e *HealthEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *e
	if cp.ID == "" {
		cp.ID = randomID("he")
	}
	if cp.TS.IsZero() {
		cp.TS = time.Now().UTC()
	}
	m.health = append(m.health, &cp)
	return nil
}

func (m *Memory) HealthStatsSince(_ context.Context, from time.Time) (HealthStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out HealthStats
	for _, e := range m.health {
		if e.TS.Before(from) {
			continue
		}
		switch e.Kind {
		case "crash":
			out.Crashes++
		case "reconnect_attempt":
			out.ReconnectAttempts++
		case "reconnect_success":
			out.ReconnectSuccess++
		}
	}
	return out, nil
}

func (m *Memory) GetAccountNote(_ context.Context, accountID string) (*AccountNote, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.notes[accountID]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *n
	return &cp, nil
}

func (m *Memory) UpsertAccountNote(_ context.Context, n *AccountNote) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.notes == nil {
		m.notes = make(map[string]*AccountNote)
	}
	cp := *n
	cp.UpdatedAt = time.Now().UTC()
	m.notes[cp.AccountID] = &cp
	return nil
}

func (m *Memory) ListSessionsByAccount(_ context.Context, accountID string, limit int) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	owned := map[string]bool{}
	for _, d := range m.devices {
		if d.AccountID == accountID {
			owned[d.ID] = true
		}
	}
	var out []*Session
	for _, s := range m.sessions {
		if owned[s.ControllerID] || owned[s.TargetID] {
			cp := *s
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) TrendBuckets(_ context.Context, from time.Time, bucket time.Duration) ([]TrendPoint, error) {
	if bucket <= 0 {
		bucket = time.Hour
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	buckets := map[int64]*TrendPoint{}
	slot := func(t time.Time) *TrendPoint {
		start := t.UTC().Truncate(bucket)
		key := start.Unix()
		p := buckets[key]
		if p == nil {
			p = &TrendPoint{BucketStart: start}
			buckets[key] = p
		}
		return p
	}
	for _, sess := range m.sessions {
		if sess.StartedAt.Before(from) {
			continue
		}
		slot(sess.StartedAt).SessionsStarted++
	}
	for _, a := range m.connectAttempts {
		if a.CreatedAt.Before(from) {
			continue
		}
		p := slot(a.CreatedAt)
		p.ConnectAttempts++
		if a.Outcome == "active" {
			p.ConnectActive++
		}
	}
	keys := make([]int64, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]TrendPoint, 0, len(keys))
	for _, k := range keys {
		out = append(out, *buckets[k])
	}
	return out, nil
}

func (m *Memory) CreateConnectAttempt(_ context.Context, a *ConnectAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.connectAttempts == nil {
		m.connectAttempts = make(map[string]*ConnectAttempt)
	}
	cp := *a
	if cp.Outcome == "" {
		cp.Outcome = "requested"
	}
	if cp.Link == "" {
		cp.Link = "unknown"
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	cp.UpdatedAt = cp.CreatedAt
	m.connectAttempts[cp.SessionID] = &cp
	return nil
}

func (m *Memory) MarkConnectAttemptActive(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.connectAttempts[sessionID]
	if !ok {
		return ErrNotFound
	}
	a.Outcome = "active"
	a.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Memory) MarkConnectAttemptRelay(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.connectAttempts[sessionID]
	if !ok {
		return ErrNotFound
	}
	a.Link = "relay"
	a.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Memory) ConnectAttemptStats(_ context.Context, from time.Time) (int, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total, active := 0, 0
	for _, a := range m.connectAttempts {
		if a.CreatedAt.Before(from) {
			continue
		}
		total++
		if a.Outcome == "active" {
			active++
		}
	}
	return total, active, nil
}

func (m *Memory) CreateKillRequest(_ context.Context, req *KillRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.killRequests == nil {
		m.killRequests = make(map[string]*KillRequest)
	}
	if req.ID == "" {
		req.ID = randomID("kr")
	}
	if req.Status == "" {
		req.Status = "pending"
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}
	cp := *req
	m.killRequests[cp.ID] = &cp
	return nil
}

func (m *Memory) GetKillRequest(_ context.Context, id string) (*KillRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.killRequests[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (m *Memory) ListKillRequests(_ context.Context, status string, p Page) ([]*KillRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*KillRequest, 0, len(m.killRequests))
	for _, r := range m.killRequests {
		if status != "" && r.Status != status {
			continue
		}
		cp := *r
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].RequestedAt.After(all[j].RequestedAt) })
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) ConfirmKillRequest(_ context.Context, id, confirmer string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.killRequests[id]
	if !ok {
		return false, ErrNotFound
	}
	if r.Status != "pending" || !r.ExpiresAt.After(now) {
		return false, nil
	}
	r.Status = "confirmed"
	r.ConfirmedBy = confirmer
	r.ConfirmedAt = now
	return true, nil
}

func (m *Memory) ExpireKillRequests(_ context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.killRequests {
		if r.Status == "pending" && !r.ExpiresAt.After(now) {
			r.Status = "expired"
			n++
		}
	}
	return n, nil
}

func (m *Memory) AddBlacklist(_ context.Context, e *BlacklistEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blacklist == nil {
		m.blacklist = make(map[string]*BlacklistEntry)
	}
	for _, existing := range m.blacklist {
		if existing.Kind == e.Kind && existing.Value == e.Value {
			return ErrConflict
		}
	}
	if e.ID == "" {
		e.ID = randomID("blk")
	}
	cp := *e
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	m.blacklist[cp.ID] = &cp
	e.ID = cp.ID
	return nil
}

func (m *Memory) ListBlacklist(_ context.Context, p Page) ([]*BlacklistEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*BlacklistEntry, 0, len(m.blacklist))
	for _, e := range m.blacklist {
		cp := *e
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) CountBlacklist(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.blacklist), nil
}

func (m *Memory) GetBlacklist(_ context.Context, id string) (*BlacklistEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.blacklist[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *e
	return &cp, nil
}

func (m *Memory) RemoveBlacklist(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blacklist[id]; !ok {
		return ErrNotFound
	}
	delete(m.blacklist, id)
	return nil
}

func (m *Memory) FindActiveBlacklist(_ context.Context, kind, value string, now time.Time) (*BlacklistEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.blacklist {
		if e.Kind != kind || e.Value != value {
			continue
		}
		if e.ExpiresAt != nil && !e.ExpiresAt.After(now) {
			continue
		}
		cp := *e
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *Memory) MatchIPBlacklist(_ context.Context, ip string, now time.Time) (*BlacklistEntry, error) {
	parsed := net.ParseIP(ip)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.blacklist {
		if e.Kind != "ip" {
			continue
		}
		if e.ExpiresAt != nil && !e.ExpiresAt.After(now) {
			continue
		}
		if e.Value == ip {
			cp := *e
			return &cp, nil
		}
		if parsed != nil && strings.Contains(e.Value, "/") {
			if _, cidr, err := net.ParseCIDR(e.Value); err == nil && cidr.Contains(parsed) {
				cp := *e
				return &cp, nil
			}
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) RecordAlert(_ context.Context, a *RiskAlert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = randomID("alert")
	}
	cp := *a
	if cp.TS.IsZero() {
		cp.TS = time.Now().UTC()
	}
	m.alerts = append(m.alerts, &cp)
	return nil
}

func (m *Memory) ListAlerts(_ context.Context, p Page) ([]*RiskAlert, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*RiskAlert, 0, len(m.alerts))
	for i := len(m.alerts) - 1; i >= 0; i-- {
		cp := *m.alerts[i]
		all = append(all, &cp)
	}
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) CountAlerts(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.alerts), nil
}

func (m *Memory) ListVersions(_ context.Context, p Page) ([]*ClientVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*ClientVersion, 0, len(m.versions))
	for _, v := range m.versions {
		cp := *v
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Platform != all[j].Platform {
			return all[i].Platform < all[j].Platform
		}
		return all[i].Version > all[j].Version
	})
	limit, offset := pageBounds(p, 100)
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *Memory) CountVersions(_ context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.versions), nil
}

func (m *Memory) GetVersion(_ context.Context, id string) (*ClientVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.versions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *Memory) UpsertVersion(_ context.Context, v *ClientVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.versions == nil {
		m.versions = make(map[string]*ClientVersion)
	}
	for _, existing := range m.versions {
		if existing.Platform == v.Platform && existing.Version == v.Version {
			cp := *v
			cp.ID = existing.ID
			cp.CreatedAt = existing.CreatedAt
			cp.UpdatedAt = time.Now().UTC()
			m.versions[existing.ID] = &cp
			v.ID = existing.ID
			return nil
		}
	}
	if v.ID == "" {
		v.ID = randomID("ver")
	}
	cp := *v
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	cp.UpdatedAt = time.Now().UTC()
	m.versions[cp.ID] = &cp
	v.ID = cp.ID
	return nil
}

func (m *Memory) UpdateVersionStatus(_ context.Context, id, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.versions[id]
	if !ok {
		return ErrNotFound
	}
	v.Status = status
	v.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *Memory) QualityStatsSince(_ context.Context, from time.Time) (QualityStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var lat []int
	relay, total := 0, 0
	for _, bySecond := range m.quality {
		for _, s := range bySecond {
			if s.TS.Before(from) {
				continue
			}
			lat = append(lat, s.LatencyMs)
			if s.Relay {
				relay++
			}
			total++
		}
	}
	out := QualityStats{Samples: total}
	if total == 0 {
		return out, nil
	}
	sort.Ints(lat)
	out.LatencyP50Ms = percentile(lat, 0.50)
	out.LatencyP95Ms = percentile(lat, 0.95)
	out.RelayPercent = int(float64(relay) / float64(total) * 100)
	return out, nil
}

func (m *Memory) QualityLinkStats(_ context.Context, from time.Time) (QualityLinkStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out QualityLinkStats
	for _, bySecond := range m.quality {
		for _, s := range bySecond {
			if s.TS.Before(from) {
				continue
			}
			switch s.Link {
			case "p2p":
				out.P2P++
			case "relay":
				out.Relay++
			}
			if s.TargetFps > 0 {
				out.FpsTotal++
				if s.Fps >= s.TargetFps {
					out.FpsMet++
				}
			}
		}
	}
	return out, nil
}

// percentile 用最近秩法取分位数（样本量小、实现可复算）。
func percentile(sorted []int, p float64) int {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted))*p+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (m *Memory) AggregateDeviceQuality(_ context.Context, deviceID string, from time.Time) ([]DeviceQualityDay, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type acc struct {
		lat, loss, relay, n int
	}
	buckets := map[string]*acc{}
	for _, sess := range m.sessions {
		if sess.ControllerID != deviceID && sess.TargetID != deviceID {
			continue
		}
		for _, s := range m.quality[sess.ID] {
			if s.TS.Before(from) {
				continue
			}
			day := s.TS.UTC().Format("2006-01-02")
			b := buckets[day]
			if b == nil {
				b = &acc{}
				buckets[day] = b
			}
			b.lat += s.LatencyMs
			b.loss += s.LossPermille
			if s.Relay {
				b.relay++
			}
			b.n++
		}
	}
	days := make([]string, 0, len(buckets))
	for d := range buckets {
		days = append(days, d)
	}
	sort.Strings(days)
	out := make([]DeviceQualityDay, 0, len(days))
	for _, d := range days {
		b := buckets[d]
		relayPct := 0
		if b.n > 0 {
			relayPct = int(float64(b.relay) / float64(b.n) * 100)
		}
		out = append(out, DeviceQualityDay{
			Date:            d,
			AvgLatencyMs:    b.lat / maxInt(b.n, 1),
			AvgLossPermille: b.loss / maxInt(b.n, 1),
			RelayPercent:    relayPct,
			Samples:         b.n,
		})
	}
	return out, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m *Memory) ListAuditActions(_ context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]struct{}, len(m.audits))
	for _, a := range m.audits {
		seen[a.Action] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for action := range seen {
		out = append(out, action)
	}
	sort.Strings(out)
	return out, nil
}

func (m *Memory) CountAudit(_ context.Context, p Page) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q := strings.ToLower(p.Query)
	n := 0
	for _, a := range m.audits {
		if p.Action != "" && a.Action != p.Action {
			continue
		}
		if p.Result != "" && a.Result != p.Result {
			continue
		}
		if p.Actor != "" && a.Actor != p.Actor {
			continue
		}
		if p.Object != "" && a.Object != p.Object {
			continue
		}
		if !p.From.IsZero() && a.TS.Before(p.From) {
			continue
		}
		if !p.To.IsZero() && a.TS.After(p.To) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.Actor+" "+a.Action+" "+a.Object+" "+a.Result), q) {
			continue
		}
		n++
	}
	return n, nil
}
