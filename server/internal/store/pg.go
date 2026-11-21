package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PG 是 Store 的 PostgreSQL 实现。
// 与内存实现共享同一套契约测试（storetest），语义必须一致。
type PG struct {
	pool *pgxpool.Pool
}

var _ Store = (*PG)(nil)

// NewPG 建立连接池并校验连通性。
func NewPG(ctx context.Context, dsn string) (*PG, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping failed: %w", err)
	}
	return &PG{pool: pool}, nil
}

// Migrate 执行内嵌的迁移脚本（幂等）。
//
// 用 PostgreSQL 咨询锁串行化：多个实例同时启动时，
// 并发执行 CREATE TABLE/TRIGGER 会触发 deadlock detected（实测 40P01）。
func (p *PG) Migrate(ctx context.Context, ddl string) error {
	const lockKey int64 = 0x45524D49 // "ERMI"
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	_, err = conn.Exec(ctx, ddl)
	return err
}

func (p *PG) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *PG) Close() error {
	p.pool.Close()
	return nil
}

// mapErr 把 pgx 的「无行」与唯一键冲突映射成 store 的哨兵错误；
// 其余错误原样返回，绝不把数据库故障伪装成「不存在」。
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return ErrConflict
	}
	return err
}

func pageArgs(p Page, def int) (int, int) {
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

// ---------- accounts ----------

func (p *PG) CreateAccount(ctx context.Context, a *Account) error {
	if a.ID == "" {
		a.ID = randomID("acc")
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = "active"
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO accounts (id, phone_hash, phone_enc, status, created_at) VALUES ($1,$2,$3,$4,$5)`,
		a.ID, a.PhoneHash, a.PhoneEnc, a.Status, a.CreatedAt)
	return mapErr(err)
}

func (p *PG) GetAccountByPhoneHash(ctx context.Context, hash string) (*Account, error) {
	return p.scanAccount(p.pool.QueryRow(ctx,
		`SELECT id, phone_hash, phone_enc, status, created_at FROM accounts WHERE phone_hash=$1`, hash))
}

func (p *PG) GetAccount(ctx context.Context, id string) (*Account, error) {
	return p.scanAccount(p.pool.QueryRow(ctx,
		`SELECT id, phone_hash, phone_enc, status, created_at FROM accounts WHERE id=$1`, id))
}

func (p *PG) scanAccount(row pgx.Row) (*Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.PhoneHash, &a.PhoneEnc, &a.Status, &a.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &a, nil
}

func (p *PG) UpdateAccountStatus(ctx context.Context, id, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE accounts SET status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) ListAccounts(ctx context.Context, page Page) ([]*Account, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		`SELECT id, phone_hash, phone_enc, status, created_at FROM accounts ORDER BY created_at ASC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.PhoneHash, &a.PhoneEnc, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// ---------- devices ----------

func (p *PG) CreateDevice(ctx context.Context, d *Device) error {
	if d.ID == "" {
		d.ID = randomID("dev")
	}
	if d.LastSeenAt.IsZero() {
		d.LastSeenAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO devices (id, device_code, account_id, platform, last_seen_at, online, token_hash, identity_pubkey)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		d.ID, d.DeviceCode, d.AccountID, d.Platform, d.LastSeenAt, d.Online, d.TokenHash, d.IdentityPubKey)
	return mapErr(err)
}

func (p *PG) scanDevice(row pgx.Row) (*Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.DeviceCode, &d.AccountID, &d.Platform, &d.LastSeenAt, &d.Online, &d.TokenHash, &d.IdentityPubKey)
	if err != nil {
		return nil, mapErr(err)
	}
	return &d, nil
}

const deviceCols = `id, device_code, account_id, platform, last_seen_at, online, token_hash, identity_pubkey`

func (p *PG) GetDeviceByCode(ctx context.Context, code string) (*Device, error) {
	return p.scanDevice(p.pool.QueryRow(ctx, `SELECT `+deviceCols+` FROM devices WHERE device_code=$1`, code))
}

func (p *PG) GetDevice(ctx context.Context, id string) (*Device, error) {
	return p.scanDevice(p.pool.QueryRow(ctx, `SELECT `+deviceCols+` FROM devices WHERE id=$1`, id))
}

func (p *PG) ListDevicesByAccount(ctx context.Context, accountID string, page Page) ([]*Device, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		`SELECT `+deviceCols+` FROM devices WHERE account_id=$1 ORDER BY device_code ASC LIMIT $2 OFFSET $3`,
		accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanDevices(rows)
}

func (p *PG) ListDevices(ctx context.Context, page Page) ([]*Device, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		`SELECT `+deviceCols+` FROM devices ORDER BY device_code ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanDevices(rows)
}

func scanDevices(rows pgx.Rows) ([]*Device, error) {
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.DeviceCode, &d.AccountID, &d.Platform, &d.LastSeenAt, &d.Online, &d.TokenHash, &d.IdentityPubKey); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (p *PG) UpdateDeviceSeen(ctx context.Context, id string, at time.Time, online bool) error {
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET last_seen_at=$2, online=$3 WHERE id=$1`, id, at, online)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) UpdateDeviceToken(ctx context.Context, id, tokenHash string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET token_hash=$2 WHERE id=$1`, id, tokenHash)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) UpdateDeviceIdentity(ctx context.Context, id, pubkey string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET identity_pubkey=$2 WHERE id=$1`, id, pubkey)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) GetDeviceIdentity(ctx context.Context, id string) (string, error) {
	var pub string
	err := p.pool.QueryRow(ctx, `SELECT identity_pubkey FROM devices WHERE id=$1`, id).Scan(&pub)
	if err != nil {
		return "", mapErr(err)
	}
	return pub, nil
}

// ---------- verify codes ----------

func (p *PG) UpsertVerifyCode(ctx context.Context, v *VerifyCode) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO verify_codes (device_code, code_hash, expires_at, attempts, locked_till)
		 VALUES ($1,$2,$3,$4,NULLIF($5, '0001-01-01 00:00:00+00'::timestamptz))
		 ON CONFLICT (device_code) DO UPDATE
		 SET code_hash=$2, expires_at=$3, attempts=$4, locked_till=EXCLUDED.locked_till`,
		v.DeviceCode, v.CodeHash, v.ExpiresAt, v.Attempts, v.LockedTill)
	return mapErr(err)
}

func (p *PG) GetVerifyCode(ctx context.Context, deviceCode string) (*VerifyCode, error) {
	var v VerifyCode
	var locked *time.Time
	err := p.pool.QueryRow(ctx,
		`SELECT device_code, code_hash, expires_at, attempts, locked_till FROM verify_codes WHERE device_code=$1`,
		deviceCode).Scan(&v.DeviceCode, &v.CodeHash, &v.ExpiresAt, &v.Attempts, &locked)
	if err != nil {
		return nil, mapErr(err)
	}
	if locked != nil {
		v.LockedTill = *locked
	}
	return &v, nil
}

// VerifyAndConsume 用单条 UPDATE 保证原子性：
//   - 记录不存在 / 已过期 → 返回 expired
//   - 处于锁定窗口 → 返回 locked
//   - 错误 → attempts+1（到阈值同时设 locked_till）
//   - 正确 → 把 expires_at 推到过去，实现「消费」
func (p *PG) VerifyAndConsume(ctx context.Context, deviceCode, codeHash string, now time.Time, maxAttempts int, lockFor time.Duration) (VerifyResult, error) {
	// 先尝试正确路径（原子消费）
	var consumed int
	err := p.pool.QueryRow(ctx, `
		UPDATE verify_codes
		SET expires_at = $3 - interval '1 second'
		WHERE device_code = $1
		  AND code_hash = $2
		  AND expires_at > $3
		  AND (locked_till IS NULL OR locked_till <= $3)
		RETURNING attempts`, deviceCode, codeHash, now).Scan(&consumed)
	if err == nil {
		return VerifyResult{Outcome: VerifyOK, Attempts: consumed}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return VerifyResult{}, mapErr(err)
	}

	// 错误路径：原子自增并可能锁定
	var attempts int
	var lockedTill *time.Time
	err = p.pool.QueryRow(ctx, `
		UPDATE verify_codes
		SET attempts = attempts + 1,
		    locked_till = CASE WHEN attempts + 1 >= $4 THEN $3 + $5::interval ELSE locked_till END
		WHERE device_code = $1
		  AND code_hash <> $2
		  AND expires_at > $3
		  AND (locked_till IS NULL OR locked_till <= $3)
		RETURNING attempts, locked_till`,
		deviceCode, codeHash, now, maxAttempts, fmt.Sprintf("%d seconds", int(lockFor.Seconds()))).
		Scan(&attempts, &lockedTill)
	if err == nil {
		out := VerifyInvalid
		if lockedTill != nil && now.Before(*lockedTill) {
			out = VerifyLocked
		}
		res := VerifyResult{Outcome: out, Attempts: attempts}
		if lockedTill != nil {
			res.LockedTo = *lockedTill
		}
		return res, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return VerifyResult{}, mapErr(err)
	}

	// 既没消费也没自增：判断是「锁定中」还是「不存在/过期」
	var locked *time.Time
	var exp time.Time
	err = p.pool.QueryRow(ctx,
		`SELECT locked_till, expires_at FROM verify_codes WHERE device_code=$1`, deviceCode).Scan(&locked, &exp)
	if errors.Is(err, pgx.ErrNoRows) {
		return VerifyResult{Outcome: VerifyExpired}, nil
	}
	if err != nil {
		return VerifyResult{}, mapErr(err)
	}
	if locked != nil && now.Before(*locked) {
		return VerifyResult{Outcome: VerifyLocked, LockedTo: *locked}, nil
	}
	return VerifyResult{Outcome: VerifyExpired}, nil
}

// ---------- trust ----------

func (p *PG) UpsertTrust(ctx context.Context, tr *TrustEdge) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO trust_edges (controller_id, target_id, expires_at) VALUES ($1,$2,$3)
		ON CONFLICT (controller_id, target_id) DO UPDATE SET expires_at=$3`,
		tr.ControllerID, tr.TargetID, tr.ExpiresAt)
	return mapErr(err)
}

func (p *PG) HasTrust(ctx context.Context, controllerID, targetID string, now time.Time) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM trust_edges WHERE controller_id=$1 AND target_id=$2 AND expires_at > $3)`,
		controllerID, targetID, now).Scan(&exists)
	if err != nil {
		return false, mapErr(err)
	}
	return exists, nil
}

// ---------- sessions ----------

func (p *PG) CreateSession(ctx context.Context, s *Session) error {
	if s.ID == "" {
		s.ID = randomID("ses")
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO sessions (id, controller_id, target_id, state, reason, started_at, ended_at, auth_expire_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,'0001-01-01 00:00:00+00'::timestamptz),NULLIF($8,'0001-01-01 00:00:00+00'::timestamptz))`,
		s.ID, s.ControllerID, s.TargetID, s.State, s.Reason, s.StartedAt, s.EndedAt, s.AuthExpireAt)
	return mapErr(err)
}

func (p *PG) scanSession(row pgx.Row) (*Session, error) {
	var s Session
	var endedAt, authExp *time.Time
	err := row.Scan(&s.ID, &s.ControllerID, &s.TargetID, &s.State, &s.Reason, &s.StartedAt, &endedAt, &authExp)
	if err != nil {
		return nil, mapErr(err)
	}
	if endedAt != nil {
		s.EndedAt = *endedAt
	}
	if authExp != nil {
		s.AuthExpireAt = *authExp
	}
	return &s, nil
}

const sessionCols = `id, controller_id, target_id, state, reason, started_at, ended_at, auth_expire_at`

func (p *PG) GetSession(ctx context.Context, id string) (*Session, error) {
	return p.scanSession(p.pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id=$1`, id))
}

func (p *PG) UpdateSession(ctx context.Context, s *Session) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE sessions SET state=$2, reason=$3, ended_at=NULLIF($4,'0001-01-01 00:00:00+00'::timestamptz),
		       auth_expire_at=NULLIF($5,'0001-01-01 00:00:00+00'::timestamptz)
		WHERE id=$1`, s.ID, s.State, s.Reason, s.EndedAt, s.AuthExpireAt)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) ListSessions(ctx context.Context, state string, page Page) ([]*Session, error) {
	limit, offset := pageArgs(page, 100)
	var rows pgx.Rows
	var err error
	if state == "" {
		rows, err = p.pool.Query(ctx,
			`SELECT `+sessionCols+` FROM sessions ORDER BY started_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	} else {
		rows, err = p.pool.Query(ctx,
			`SELECT `+sessionCols+` FROM sessions WHERE state=$1 ORDER BY started_at DESC LIMIT $2 OFFSET $3`,
			state, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		var s Session
		var endedAt, authExp *time.Time
		if err := rows.Scan(&s.ID, &s.ControllerID, &s.TargetID, &s.State, &s.Reason, &s.StartedAt, &endedAt, &authExp); err != nil {
			return nil, err
		}
		if endedAt != nil {
			s.EndedAt = *endedAt
		}
		if authExp != nil {
			s.AuthExpireAt = *authExp
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// ---------- files ----------

func (p *PG) CreateFile(ctx context.Context, f *FileMeta) error {
	if f.ID == "" {
		f.ID = randomID("fil")
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO files (id, account_id, device_id, name, size, sha256, storage_path, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		f.ID, f.AccountID, f.DeviceID, f.Name, f.Size, f.SHA256, f.StoragePath, f.CreatedAt)
	return mapErr(err)
}

const fileCols = `id, account_id, device_id, name, size, sha256, storage_path, created_at`

func (p *PG) scanFile(row pgx.Row) (*FileMeta, error) {
	var f FileMeta
	err := row.Scan(&f.ID, &f.AccountID, &f.DeviceID, &f.Name, &f.Size, &f.SHA256, &f.StoragePath, &f.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &f, nil
}

func (p *PG) GetFile(ctx context.Context, id string) (*FileMeta, error) {
	return p.scanFile(p.pool.QueryRow(ctx, `SELECT `+fileCols+` FROM files WHERE id=$1`, id))
}

func (p *PG) ListFilesByAccount(ctx context.Context, accountID string, page Page) ([]*FileMeta, error) {
	limit, offset := pageArgs(page, 20)
	rows, err := p.pool.Query(ctx, `
		SELECT `+fileCols+` FROM files WHERE account_id=$1
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*FileMeta
	for rows.Next() {
		var f FileMeta
		if err := rows.Scan(&f.ID, &f.AccountID, &f.DeviceID, &f.Name, &f.Size, &f.SHA256, &f.StoragePath, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (p *PG) DeleteFile(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM files WHERE id=$1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------- assist codes ----------

func (p *PG) PutAssistCode(ctx context.Context, c *AssistCode) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO assist_codes (code, account_id, target_id, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (code) DO UPDATE
		SET account_id=$2, target_id=$3, created_at=$4, expires_at=$5, redeemed_at=NULL`,
		c.Code, c.AccountID, c.TargetID, c.CreatedAt, c.ExpiresAt)
	return mapErr(err)
}

func (p *PG) GetAssistCode(ctx context.Context, code string) (*AssistCode, error) {
	var c AssistCode
	var redeemed *time.Time
	err := p.pool.QueryRow(ctx, `
		SELECT code, account_id, target_id, created_at, expires_at, redeemed_at
		FROM assist_codes WHERE code=$1`, code).
		Scan(&c.Code, &c.AccountID, &c.TargetID, &c.CreatedAt, &c.ExpiresAt, &redeemed)
	if err != nil {
		return nil, mapErr(err)
	}
	if redeemed != nil {
		c.RedeemedAt = *redeemed
	}
	return &c, nil
}

// RedeemAssistCode 用单条 UPDATE 完成原子消费：
// 只有「未兑换 + 未过期」的行才会被标记，并发下只有一个调用者拿到 RowsAffected=1。
func (p *PG) RedeemAssistCode(ctx context.Context, code string, now time.Time) (bool, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE assist_codes SET redeemed_at=$2
		WHERE code=$1 AND redeemed_at IS NULL AND expires_at > $2`, code, now)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

// ---------- app config ----------

func (p *PG) PutConfig(ctx context.Context, key, value string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO app_config (key, value, updated_at) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value=$2, updated_at=$3`,
		key, value, time.Now().UTC())
	return mapErr(err)
}

func (p *PG) ListConfig(ctx context.Context) (map[string]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT key, value FROM app_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ---------- admin ----------

func (p *PG) GetAdminByUsername(ctx context.Context, username string) (*AdminUser, error) {
	var u AdminUser
	err := p.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, totp_secret, role, status FROM admin_users WHERE username=$1`,
		username).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.Role, &u.Status)
	if err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

func (p *PG) PutAdmin(ctx context.Context, a *AdminUser) error {
	if a.ID == "" {
		a.ID = randomID("adm")
	}
	if a.Status == "" {
		a.Status = "active"
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO admin_users (id, username, password_hash, totp_secret, role, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (username) DO UPDATE
		SET password_hash=$3, totp_secret=$4, role=$5, status=$6`,
		a.ID, a.Username, a.PasswordHash, a.TOTPSecret, a.Role, a.Status)
	return mapErr(err)
}

func (p *PG) AppendAudit(ctx context.Context, a *AuditLog) error {
	if a.ID == "" {
		a.ID = randomID("aud")
	}
	if a.TS.IsZero() {
		a.TS = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO audit_logs (id, actor, role, action, object, before, after, ip, result, ts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.ID, a.Actor, a.Role, a.Action, a.Object, a.Before, a.After, a.IP, a.Result, a.TS)
	return mapErr(err)
}

func (p *PG) ListAudit(ctx context.Context, limit int) ([]*AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, actor, role, action, object, before, after, ip, result, ts
		FROM audit_logs ORDER BY ts DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditLog
	for rows.Next() {
		var a AuditLog
		if err := rows.Scan(&a.ID, &a.Actor, &a.Role, &a.Action, &a.Object, &a.Before, &a.After, &a.IP, &a.Result, &a.TS); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// normalizeSQL 去掉多余空白，便于在日志里打印迁移语句片段。
func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
