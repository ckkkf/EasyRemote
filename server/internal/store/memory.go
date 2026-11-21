package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
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
	closed   bool
}

// NewMemory 创建内存存储。
func NewMemory() *Memory {
	return &Memory{
		accounts: map[string]*Account{},
		byPhone:  map[string]string{},
		devices:  map[string]*Device{},
		byCode:   map[string]string{},
		codes:    map[string]*VerifyCode{},
		trust:    map[string]*TrustEdge{},
		sessions: map[string]*Session{},
		admins:   map[string]*AdminUser{},
		files:    map[string]*FileMeta{},
		assist:   map[string]*AssistCode{},
		config:   map[string]string{},
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

func (m *Memory) ListAccounts(_ context.Context, p Page) ([]*Account, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*Account, 0, len(m.accounts))
	for _, a := range m.accounts {
		cp := *a
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })
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

func (m *Memory) ListDevices(_ context.Context, p Page) ([]*Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := make([]*Device, 0, len(m.devices))
	for _, d := range m.devices {
		cp := *d
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].DeviceCode < all[j].DeviceCode })
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
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
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
	cp := *s
	m.sessions[s.ID] = &cp
	return nil
}

func (m *Memory) ListSessions(_ context.Context, state string, p Page) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Session
	for _, s := range m.sessions {
		if state == "" || s.State == state {
			cp := *s
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
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
