package store

import (
	"context"
	"errors"
	"fmt"
	"net"
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

// Ping 检查数据库可用性，用于 /readyz。
func (p *PG) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

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

const accountColumns = `id, phone_hash, phone_enc, status, nickname, COALESCE(last_login_at, 'epoch'::timestamptz), created_at`

func (p *PG) GetAccountByPhoneHash(ctx context.Context, hash string) (*Account, error) {
	return p.scanAccount(p.pool.QueryRow(ctx,
		"SELECT "+accountColumns+" FROM accounts WHERE phone_hash=$1", hash))
}

func (p *PG) GetAccount(ctx context.Context, id string) (*Account, error) {
	return p.scanAccount(p.pool.QueryRow(ctx,
		"SELECT "+accountColumns+" FROM accounts WHERE id=$1", id))
}

func (p *PG) scanAccount(row pgx.Row) (*Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.PhoneHash, &a.PhoneEnc, &a.Status, &a.Nickname, &a.LastLoginAt, &a.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &a, nil
}

func (p *PG) TouchAccountLogin(ctx context.Context, id string, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `UPDATE accounts SET last_login_at=$2 WHERE id=$1`, id, at)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

func (p *PG) UpdateAccountPhoneEnc(ctx context.Context, id, phoneEnc string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE accounts SET phone_enc=$2 WHERE id=$1`, id, phoneEnc)
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
	args := []any{}
	where := ""
	if page.Status != "" {
		args = append(args, page.Status)
		where = fmt.Sprintf(" WHERE status=$%d", len(args))
	}
	if q := strings.TrimSpace(page.Query); q != "" {
		args = append(args, "%"+q+"%")
		cond := fmt.Sprintf("(id ILIKE $%d OR phone_enc ILIKE $%d OR status ILIKE $%d)", len(args), len(args), len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	// 时间范围（ADM-API-03e）：按注册时间过滤。
	if !page.From.IsZero() {
		args = append(args, page.From)
		cond := fmt.Sprintf("created_at >= $%d", len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if !page.To.IsZero() {
		args = append(args, page.To)
		cond := fmt.Sprintf("created_at <= $%d", len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	order := "created_at"
	switch page.Sort {
	case "id":
		order = "id"
	case "status":
		order = "status"
	}
	if page.Desc {
		order += " DESC"
	} else {
		order += " ASC"
	}
	args = append(args, limit, offset)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf("SELECT "+accountColumns+" FROM accounts%s ORDER BY %s LIMIT $%d OFFSET $%d", where, order, len(args)-1, len(args)),
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.PhoneHash, &a.PhoneEnc, &a.Status, &a.Nickname, &a.LastLoginAt, &a.CreatedAt); err != nil {
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
		`INSERT INTO devices (id, device_code, account_id, platform, last_seen_at, online, token_hash, identity_pubkey, device_name, os_version, client_version, nat_type)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.ID, d.DeviceCode, d.AccountID, d.Platform, d.LastSeenAt, d.Online, d.TokenHash, d.IdentityPubKey,
		d.DeviceName, d.OSVersion, d.ClientVersion, d.NATType)
	return mapErr(err)
}

func (p *PG) scanDevice(row pgx.Row) (*Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.DeviceCode, &d.AccountID, &d.Platform, &d.LastSeenAt, &d.Online, &d.TokenHash, &d.IdentityPubKey,
		&d.DeviceName, &d.OSVersion, &d.ClientVersion, &d.NATType, &d.RiskFlag, &d.RiskReason, &d.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &d, nil
}

const deviceCols = `id, device_code, account_id, platform, last_seen_at, online, token_hash, identity_pubkey, device_name, os_version, client_version, nat_type, risk_flag, risk_reason, created_at`

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
	args := []any{}
	where := ""
	if page.Platform != "" {
		args = append(args, page.Platform)
		where = fmt.Sprintf(" WHERE platform=$%d", len(args))
	}
	if page.Online != nil {
		args = append(args, *page.Online)
		cond := fmt.Sprintf("online=$%d", len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if q := strings.TrimSpace(page.Query); q != "" {
		args = append(args, "%"+q+"%")
		cond := fmt.Sprintf("(id ILIKE $%d OR device_code ILIKE $%d OR platform ILIKE $%d OR account_id ILIKE $%d)", len(args), len(args), len(args), len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	order := "device_code"
	switch page.Sort {
	case "platform":
		order = "platform"
	case "last_seen":
		order = "last_seen_at"
	}
	if page.Desc {
		order += " DESC"
	} else {
		order += " ASC"
	}
	args = append(args, limit, offset)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM devices%s ORDER BY %s LIMIT $%d OFFSET $%d", deviceCols, where, order, len(args)-1, len(args)),
		args...)
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
		if err := rows.Scan(&d.ID, &d.DeviceCode, &d.AccountID, &d.Platform, &d.LastSeenAt, &d.Online, &d.TokenHash, &d.IdentityPubKey,
			&d.DeviceName, &d.OSVersion, &d.ClientVersion, &d.NATType, &d.RiskFlag, &d.RiskReason, &d.CreatedAt); err != nil {
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

func (p *PG) UpdateDevicePlatform(ctx context.Context, id, platform string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET platform=$2 WHERE id=$1`, id, platform)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) DeleteDevice(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM devices WHERE id=$1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) SetDeviceRisk(ctx context.Context, id string, flag bool, reason string) error {
	riskReason := ""
	if flag {
		riskReason = reason
	}
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET risk_flag=$2, risk_reason=$3 WHERE id=$1`, id, flag, riskReason)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) UpdateDeviceMeta(ctx context.Context, id, deviceName, osVersion, clientVersion, natType string) error {
	tag, err := p.pool.Exec(ctx,
		`UPDATE devices SET
		   device_name = CASE WHEN $2 = '' THEN device_name ELSE $2 END,
		   os_version = CASE WHEN $3 = '' THEN os_version ELSE $3 END,
		   client_version = CASE WHEN $4 = '' THEN client_version ELSE $4 END,
		   nat_type = CASE WHEN $5 = '' THEN nat_type ELSE $5 END
		 WHERE id=$1`,
		id, deviceName, osVersion, clientVersion, natType)
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

func (p *PG) UpdateDevicePassword(ctx context.Context, id, passwordHash string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE devices SET password_hash=$2 WHERE id=$1`, id, passwordHash)
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
		INSERT INTO sessions (id, controller_id, target_id, state, reason, started_at, ended_at, auth_expire_at, version, ready_controller, ready_target, close_kind)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,'0001-01-01 00:00:00+00'::timestamptz),NULLIF($8,'0001-01-01 00:00:00+00'::timestamptz),1,false,false,$9)`,
		s.ID, s.ControllerID, s.TargetID, s.State, s.Reason, s.StartedAt, s.EndedAt, s.AuthExpireAt, s.CloseKind)
	if err == nil {
		s.Version = 1
	}
	return mapErr(err)
}

func (p *PG) scanSession(row pgx.Row) (*Session, error) {
	var s Session
	var endedAt, authExp *time.Time
	err := row.Scan(&s.ID, &s.ControllerID, &s.TargetID, &s.State, &s.Reason, &s.StartedAt, &endedAt, &authExp, &s.Version, &s.ReadyController, &s.ReadyTarget, &s.CloseKind)
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

const sessionCols = `id, controller_id, target_id, state, reason, started_at, ended_at, auth_expire_at, version, ready_controller, ready_target, close_kind`

func (p *PG) CreateSessionExclusive(ctx context.Context, s *Session) (*Session, bool, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, false, mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 按设备对串行化，避免两个并发首次请求各建一条。
	lockKey := s.ControllerID + "|" + s.TargetID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return nil, false, mapErr(err)
	}
	row := tx.QueryRow(ctx, `SELECT `+sessionCols+` FROM sessions
		WHERE controller_id=$1 AND target_id=$2 AND state <> 'closed'
		ORDER BY started_at DESC LIMIT 1`, s.ControllerID, s.TargetID)
	existing, err := p.scanSession(row)
	if err == nil {
		return existing, false, mapErr(tx.Commit(ctx))
	}
	if err != ErrNotFound {
		return nil, false, err
	}
	if s.ID == "" {
		s.ID = randomID("ses")
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now().UTC()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO sessions (id, controller_id, target_id, state, reason, started_at, ended_at, auth_expire_at, version, ready_controller, ready_target, close_kind)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,'0001-01-01 00:00:00+00'::timestamptz),NULLIF($8,'0001-01-01 00:00:00+00'::timestamptz),1,false,false,$9)`,
		s.ID, s.ControllerID, s.TargetID, s.State, s.Reason, s.StartedAt, s.EndedAt, s.AuthExpireAt, s.CloseKind)
	if err != nil {
		return nil, false, mapErr(err)
	}
	s.Version = 1
	if err := tx.Commit(ctx); err != nil {
		return nil, false, mapErr(err)
	}
	cp := *s
	return &cp, true, nil
}

func (p *PG) GetSession(ctx context.Context, id string) (*Session, error) {
	return p.scanSession(p.pool.QueryRow(ctx, `SELECT `+sessionCols+` FROM sessions WHERE id=$1`, id))
}

func (p *PG) UpdateSession(ctx context.Context, s *Session) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE sessions SET state=$2, reason=$3, ended_at=NULLIF($4,'0001-01-01 00:00:00+00'::timestamptz),
		       auth_expire_at=NULLIF($5,'0001-01-01 00:00:00+00'::timestamptz),
		       ready_controller=$6, ready_target=$7, close_kind=$8, version=version+1
		WHERE id=$1`, s.ID, s.State, s.Reason, s.EndedAt, s.AuthExpireAt, s.ReadyController, s.ReadyTarget, s.CloseKind)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	s.Version++
	return nil
}

func (p *PG) UpdateSessionCASWithEvent(ctx context.Context, s *Session, expected int64, ev *SessionEvent) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return mapErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE sessions SET state=$2, reason=$3, ended_at=NULLIF($4,'0001-01-01 00:00:00+00'::timestamptz),
		       auth_expire_at=NULLIF($5,'0001-01-01 00:00:00+00'::timestamptz),
		       ready_controller=$7, ready_target=$8, close_kind=$9, version=version+1
		WHERE id=$1 AND version=$6`, s.ID, s.State, s.Reason, s.EndedAt, s.AuthExpireAt, expected, s.ReadyController, s.ReadyTarget, s.CloseKind)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionConflict
	}
	s.Version = expected + 1
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
	_, err = tx.Exec(ctx, `
		INSERT INTO session_events (id, session_id, from_device, to_device, old_state, new_state, reason, trace_id, version, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (session_id, version) DO NOTHING`,
		ev.ID, ev.SessionID, ev.FromDevice, ev.ToDevice, ev.OldState, ev.NewState, ev.Reason, ev.TraceID, ev.Version, ev.At)
	if err != nil {
		return mapErr(err)
	}
	return mapErr(tx.Commit(ctx))
}

func (p *PG) ListSessionEvents(ctx context.Context, delivered bool, limit int) ([]*SessionEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, session_id, from_device, to_device, old_state, new_state, reason, trace_id, version, at, delivered, attempts, last_error
		FROM session_events WHERE delivered=$1 ORDER BY at ASC LIMIT $2`, delivered, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*SessionEvent
	for rows.Next() {
		var ev SessionEvent
		if err := rows.Scan(&ev.ID, &ev.SessionID, &ev.FromDevice, &ev.ToDevice, &ev.OldState, &ev.NewState,
			&ev.Reason, &ev.TraceID, &ev.Version, &ev.At, &ev.Delivered, &ev.Attempts, &ev.LastError); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &ev)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) MarkSessionEventDelivered(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE session_events SET delivered=true, last_error='' WHERE id=$1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) MarkSessionEventFailed(ctx context.Context, id, reason string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE session_events SET attempts=attempts+1, last_error=$2 WHERE id=$1`, id, reason)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) UpdateSessionCAS(ctx context.Context, s *Session, expected int64) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE sessions SET state=$2, reason=$3, ended_at=NULLIF($4,'0001-01-01 00:00:00+00'::timestamptz),
		       auth_expire_at=NULLIF($5,'0001-01-01 00:00:00+00'::timestamptz),
		       ready_controller=$7, ready_target=$8, close_kind=$9, version=version+1
		WHERE id=$1 AND version=$6`, s.ID, s.State, s.Reason, s.EndedAt, s.AuthExpireAt, expected, s.ReadyController, s.ReadyTarget, s.CloseKind)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVersionConflict
	}
	s.Version = expected + 1
	return nil
}

// sessionWhere 构造会话过滤条件；List 与 Count 必须共用，否则分页总数会和列表对不上。
func sessionWhere(state string, page Page, args *[]any) string {
	where := ""
	add := func(cond string) {
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if state != "" {
		*args = append(*args, state)
		add(fmt.Sprintf("state=$%d", len(*args)))
	}
	if q := strings.TrimSpace(page.Query); q != "" {
		*args = append(*args, "%"+q+"%")
		add(fmt.Sprintf("(id ILIKE $%d OR controller_id ILIKE $%d OR target_id ILIKE $%d OR state ILIKE $%d)", len(*args), len(*args), len(*args), len(*args)))
	}
	if !page.From.IsZero() {
		*args = append(*args, page.From)
		add(fmt.Sprintf("started_at >= $%d", len(*args)))
	}
	if !page.To.IsZero() {
		*args = append(*args, page.To)
		add(fmt.Sprintf("started_at <= $%d", len(*args)))
	}
	if page.DeviceID != "" {
		*args = append(*args, page.DeviceID)
		add(fmt.Sprintf("(controller_id=$%d OR target_id=$%d)", len(*args), len(*args)))
	}
	if page.AccountID != "" {
		*args = append(*args, page.AccountID)
		add(fmt.Sprintf("(controller_id IN (SELECT id FROM devices WHERE account_id=$%d) OR target_id IN (SELECT id FROM devices WHERE account_id=$%d))", len(*args), len(*args)))
	}
	if page.CloseKind != "" {
		*args = append(*args, page.CloseKind)
		add(fmt.Sprintf("close_kind=$%d", len(*args)))
	}
	if page.Abnormal != nil {
		// 只有已结束的会话才有"异常结束"语义。
		if *page.Abnormal {
			add("state='closed' AND close_kind NOT IN ('user_hangup','admin_kill')")
		} else {
			add("state='closed' AND close_kind IN ('user_hangup','admin_kill')")
		}
	}
	if page.Link != "" {
		*args = append(*args, page.Link)
		// 与列表"链路"列同口径：多数票，相等 mixed，无上报不计入任何一类。
		cond := fmt.Sprintf(`(SELECT CASE WHEN p2p > relay THEN 'p2p' WHEN relay > p2p THEN 'relay' ELSE 'mixed' END
			FROM (SELECT count(*) FILTER (WHERE link='p2p') AS p2p, count(*) FILTER (WHERE link='relay') AS relay
			      FROM session_quality_samples q WHERE q.session_id = sessions.id) s
			WHERE p2p + relay > 0) = $%d`, len(*args))
		add(cond)
	}
	return where
}

func (p *PG) ListSessions(ctx context.Context, state string, page Page) ([]*Session, error) {
	limit, offset := pageArgs(page, 100)
	args := []any{}
	where := sessionWhere(state, page, &args)
	order := "started_at"
	switch page.Sort {
	case "id":
		order = "id"
	case "state":
		order = "state"
	}
	if page.Desc {
		order += " DESC"
	} else {
		order += " ASC"
	}
	args = append(args, limit, offset)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf("SELECT %s FROM sessions%s ORDER BY %s LIMIT $%d OFFSET $%d", sessionCols, where, order, len(args)-1, len(args)),
		args...)
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

func (p *PG) ListConfigOverrides(ctx context.Context, platform, version string) (map[string]string, error) {
	if platform == "" && version == "" {
		return p.ListConfig(ctx)
	}
	rows, err := p.pool.Query(ctx, `SELECT key, value FROM config_overrides WHERE platform=$1 AND version=$2`, platform, version)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, mapErr(err)
		}
		out[k] = v
	}
	return out, mapErr(rows.Err())
}

func (p *PG) SetConfigOverride(ctx context.Context, key, platform, version, value string) error {
	if platform == "" && version == "" {
		return p.PutConfig(ctx, key, value)
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO config_overrides (key, platform, version, value) VALUES ($1,$2,$3,$4)
		ON CONFLICT (key, platform, version) DO UPDATE SET value=$4`,
		key, platform, version, value)
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

func (p *PG) GetAdmin(ctx context.Context, id string) (*AdminUser, error) {
	var u AdminUser
	err := p.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, totp_secret, role, status FROM admin_users WHERE id=$1`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.Role, &u.Status)
	if err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

func (p *PG) ListAdmins(ctx context.Context, page Page) ([]*AdminUser, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		`SELECT id, username, password_hash, totp_secret, role, status FROM admin_users
		 ORDER BY username ASC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.TOTPSecret, &u.Role, &u.Status); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &u)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n)
	return n, mapErr(err)
}

func (p *PG) UpdateAdminRole(ctx context.Context, id, role string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE admin_users SET role=$2 WHERE id=$1`, id, role)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) UpdateAdminStatus(ctx context.Context, id, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE admin_users SET status=$2 WHERE id=$1`, id, status)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
func (p *PG) CountAccounts(ctx context.Context, page Page) (int, error) {
	args := []any{}
	where := ""
	if page.Status != "" {
		args = append(args, page.Status)
		where = fmt.Sprintf(" WHERE status=$%d", len(args))
	}
	if !page.From.IsZero() {
		args = append(args, page.From)
		cond := fmt.Sprintf("created_at >= $%d", len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if !page.To.IsZero() {
		args = append(args, page.To)
		cond := fmt.Sprintf("created_at <= $%d", len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if q := strings.TrimSpace(page.Query); q != "" {
		args = append(args, "%"+q+"%")
		cond := fmt.Sprintf("(id ILIKE $%d OR phone_enc ILIKE $%d OR status ILIKE $%d)", len(args), len(args), len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	var n int
	err := p.pool.QueryRow(ctx, "SELECT count(*) FROM accounts"+where, args...).Scan(&n)
	return n, mapErr(err)
}

func (p *PG) DeviceCountsByAccount(ctx context.Context, accountIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx,
		`SELECT account_id, count(*) FROM devices WHERE account_id = ANY($1) GROUP BY account_id`,
		accountIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, mapErr(err)
		}
		out[id] = n
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountDevices(ctx context.Context, page Page) (int, error) {
	args := []any{}
	where := ""
	if page.Platform != "" {
		args = append(args, page.Platform)
		where = fmt.Sprintf(" WHERE platform=$%d", len(args))
	}
	if page.Online != nil {
		args = append(args, *page.Online)
		cond := fmt.Sprintf("online=$%d", len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if q := strings.TrimSpace(page.Query); q != "" {
		args = append(args, "%"+q+"%")
		cond := fmt.Sprintf("(id ILIKE $%d OR device_code ILIKE $%d OR platform ILIKE $%d OR account_id ILIKE $%d)", len(args), len(args), len(args), len(args))
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	var n int
	err := p.pool.QueryRow(ctx, "SELECT count(*) FROM devices"+where, args...).Scan(&n)
	return n, mapErr(err)
}

func (p *PG) CountSessions(ctx context.Context, state string, page Page) (int, error) {
	args := []any{}
	where := sessionWhere(state, page, &args)
	var n int
	err := p.pool.QueryRow(ctx, "SELECT count(*) FROM sessions"+where, args...).Scan(&n)
	return n, mapErr(err)
}

// auditWhere 构造审计过滤条件；List 与 Count 必须共用，否则分页总数会和列表对不上。
func auditWhere(page Page, args *[]any) string {
	where := ""
	add := func(cond string) {
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	if page.Action != "" {
		*args = append(*args, page.Action)
		add(fmt.Sprintf("action=$%d", len(*args)))
	}
	if page.Result != "" {
		*args = append(*args, page.Result)
		add(fmt.Sprintf("result=$%d", len(*args)))
	}
	if page.Actor != "" {
		*args = append(*args, page.Actor)
		add(fmt.Sprintf("actor=$%d", len(*args)))
	}
	if page.Object != "" {
		*args = append(*args, page.Object)
		add(fmt.Sprintf("object=$%d", len(*args)))
	}
	if !page.From.IsZero() {
		*args = append(*args, page.From)
		add(fmt.Sprintf("ts >= $%d", len(*args)))
	}
	if !page.To.IsZero() {
		*args = append(*args, page.To)
		add(fmt.Sprintf("ts <= $%d", len(*args)))
	}
	if q := strings.TrimSpace(page.Query); q != "" {
		*args = append(*args, "%"+q+"%")
		add(fmt.Sprintf("(actor ILIKE $%d OR action ILIKE $%d OR object ILIKE $%d OR result ILIKE $%d)", len(*args), len(*args), len(*args), len(*args)))
	}
	return where
}

func (p *PG) AppendQualitySamples(ctx context.Context, samples []QualitySample) error {
	if len(samples) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, s := range samples {
		if s.SessionID == "" {
			continue
		}
		batch.Queue(
			`INSERT INTO session_quality_samples (session_id, ts, latency_ms, loss_permille, fps, tier, relay, link, target_fps)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			 ON CONFLICT (session_id, ts) DO UPDATE SET
			   latency_ms=EXCLUDED.latency_ms, loss_permille=EXCLUDED.loss_permille,
			   fps=EXCLUDED.fps, tier=EXCLUDED.tier, relay=EXCLUDED.relay,
			   link=EXCLUDED.link, target_fps=EXCLUDED.target_fps`,
			s.SessionID, s.TS, s.LatencyMs, s.LossPermille, s.Fps, s.Tier, s.Relay, s.Link, s.TargetFps)
	}
	if batch.Len() == 0 {
		return nil
	}
	br := p.pool.SendBatch(ctx, batch)
	if err := br.Close(); err != nil {
		return mapErr(err)
	}
	return nil
}

func (p *PG) ListQualitySamples(ctx context.Context, sessionID string, limit int) ([]QualitySample, error) {
	if limit <= 0 || limit > 3600 {
		limit = 600
	}
	rows, err := p.pool.Query(ctx,
		`SELECT session_id, ts, latency_ms, loss_permille, fps, tier, relay, link, target_fps
		 FROM session_quality_samples WHERE session_id=$1 ORDER BY ts ASC LIMIT $2`,
		sessionID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []QualitySample
	for rows.Next() {
		var s QualitySample
		if err := rows.Scan(&s.SessionID, &s.TS, &s.LatencyMs, &s.LossPermille, &s.Fps, &s.Tier, &s.Relay, &s.Link, &s.TargetFps); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, s)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) SessionSummaries(ctx context.Context, sessionIDs []string) (map[string]SessionSummary, error) {
	out := make(map[string]SessionSummary, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `
		SELECT session_id,
		       count(*)::int,
		       round(avg(latency_ms))::int,
		       count(*) FILTER (WHERE link='p2p')::int,
		       count(*) FILTER (WHERE link='relay')::int
		FROM session_quality_samples
		WHERE session_id = ANY($1)
		GROUP BY session_id`, sessionIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var s SessionSummary
		if err := rows.Scan(&id, &s.Samples, &s.AvgLatencyMs, &s.P2PCount, &s.RelayCount); err != nil {
			return nil, mapErr(err)
		}
		out[id] = s
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountDevicesSince(ctx context.Context, from time.Time) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE created_at >= $1`, from).Scan(&n)
	return n, mapErr(err)
}

func (p *PG) RecordSessionError(ctx context.Context, e *SessionError) error {
	if e.ID == "" {
		e.ID = randomID("se")
	}
	ts := e.TS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO session_errors (id, session_id, code, name, detail, source, ts) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ID, e.SessionID, e.Code, e.Name, e.Detail, e.Source, ts)
	return mapErr(err)
}

func (p *PG) ListSessionErrors(ctx context.Context, sessionID string, limit int) ([]*SessionError, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx,
		`SELECT id, session_id, code, name, detail, source, ts FROM session_errors
		 WHERE session_id=$1 ORDER BY ts ASC LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*SessionError
	for rows.Next() {
		var e SessionError
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Code, &e.Name, &e.Detail, &e.Source, &e.TS); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &e)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) RecordHealthEvent(ctx context.Context, e *HealthEvent) error {
	if e.ID == "" {
		e.ID = randomID("he")
	}
	ts := e.TS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO client_health_events (id, session_id, device_id, kind, ts) VALUES ($1,$2,$3,$4,$5)`,
		e.ID, e.SessionID, e.DeviceID, e.Kind, ts)
	return mapErr(err)
}

func (p *PG) HealthStatsSince(ctx context.Context, from time.Time) (HealthStats, error) {
	var out HealthStats
	err := p.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE kind='crash'),
		       count(*) FILTER (WHERE kind='reconnect_attempt'),
		       count(*) FILTER (WHERE kind='reconnect_success')
		FROM client_health_events WHERE ts >= $1`, from).
		Scan(&out.Crashes, &out.ReconnectAttempts, &out.ReconnectSuccess)
	return out, mapErr(err)
}

func (p *PG) GetAccountNote(ctx context.Context, accountID string) (*AccountNote, error) {
	var n AccountNote
	err := p.pool.QueryRow(ctx,
		`SELECT account_id, note, updated_by, updated_at FROM account_notes WHERE account_id=$1`, accountID).
		Scan(&n.AccountID, &n.Note, &n.UpdatedBy, &n.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &n, nil
}

func (p *PG) UpsertAccountNote(ctx context.Context, n *AccountNote) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO account_notes (account_id, note, updated_by, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (account_id) DO UPDATE SET note=$2, updated_by=$3, updated_at=now()`,
		n.AccountID, n.Note, n.UpdatedBy)
	return mapErr(err)
}

func (p *PG) ListSessionsByAccount(ctx context.Context, accountID string, limit int) ([]*Session, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		SELECT s.id, s.controller_id, s.target_id, s.state, s.reason, s.started_at, s.ended_at,
		       s.auth_expire_at, s.version, s.ready_controller, s.ready_target
		FROM sessions s
		WHERE s.controller_id IN (SELECT id FROM devices WHERE account_id=$1)
		   OR s.target_id IN (SELECT id FROM devices WHERE account_id=$1)
		ORDER BY s.started_at DESC LIMIT $2`, accountID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		var s Session
		var endedAt, authExpireAt *time.Time
		if err := rows.Scan(&s.ID, &s.ControllerID, &s.TargetID, &s.State, &s.Reason, &s.StartedAt,
			&endedAt, &authExpireAt, &s.Version, &s.ReadyController, &s.ReadyTarget); err != nil {
			return nil, mapErr(err)
		}
		if endedAt != nil {
			s.EndedAt = *endedAt
		}
		if authExpireAt != nil {
			s.AuthExpireAt = *authExpireAt
		}
		out = append(out, &s)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) TrendBuckets(ctx context.Context, from time.Time, bucket time.Duration) ([]TrendPoint, error) {
	secs := int(bucket.Seconds())
	if secs <= 0 {
		secs = 3600
	}
	rows, err := p.pool.Query(ctx, `
		WITH buckets AS (
		  SELECT to_timestamp(floor(extract(epoch FROM started_at) / $2) * $2) AS b, count(*) AS n
		  FROM sessions WHERE started_at >= $1 GROUP BY b
		), attempts AS (
		  SELECT to_timestamp(floor(extract(epoch FROM created_at) / $2) * $2) AS b,
		         count(*) AS n, count(*) FILTER (WHERE outcome='active') AS active
		  FROM connect_attempts WHERE created_at >= $1 GROUP BY b
		)
		SELECT COALESCE(buckets.b, attempts.b) AS bucket_start,
		       COALESCE(buckets.n, 0)::int,
		       COALESCE(attempts.n, 0)::int,
		       COALESCE(attempts.active, 0)::int
		FROM buckets FULL OUTER JOIN attempts ON buckets.b = attempts.b
		ORDER BY bucket_start ASC`, from, secs)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []TrendPoint
	for rows.Next() {
		var p TrendPoint
		if err := rows.Scan(&p.BucketStart, &p.SessionsStarted, &p.ConnectAttempts, &p.ConnectActive); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, p)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CreateConnectAttempt(ctx context.Context, a *ConnectAttempt) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO connect_attempts (session_id, account_id, controller_id, target_id, outcome, link)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (session_id) DO NOTHING`,
		a.SessionID, a.AccountID, a.ControllerID, a.TargetID,
		coalesce(a.Outcome, "requested"), coalesce(a.Link, "unknown"))
	return mapErr(err)
}

func coalesce(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func (p *PG) MarkConnectAttemptActive(ctx context.Context, sessionID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE connect_attempts SET outcome='active', updated_at=now() WHERE session_id=$1`, sessionID)
	return mapErr(err)
}

func (p *PG) MarkConnectAttemptRelay(ctx context.Context, sessionID string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE connect_attempts SET link='relay', updated_at=now() WHERE session_id=$1`, sessionID)
	return mapErr(err)
}

func (p *PG) ConnectAttemptStats(ctx context.Context, from time.Time) (int, int, error) {
	var total, active int
	err := p.pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE outcome='active')
		FROM connect_attempts WHERE created_at >= $1`, from).Scan(&total, &active)
	return total, active, mapErr(err)
}

const killRequestCols = `id, session_id, reason, requested_by, requested_at, expires_at, status, confirmed_by, confirmed_at`

func scanKillRequest(row interface{ Scan(...any) error }) (*KillRequest, error) {
	var r KillRequest
	var confirmedAt *time.Time
	if err := row.Scan(&r.ID, &r.SessionID, &r.Reason, &r.RequestedBy, &r.RequestedAt, &r.ExpiresAt,
		&r.Status, &r.ConfirmedBy, &confirmedAt); err != nil {
		return nil, mapErr(err)
	}
	if confirmedAt != nil {
		r.ConfirmedAt = *confirmedAt
	}
	return &r, nil
}

func (p *PG) CreateKillRequest(ctx context.Context, req *KillRequest) error {
	if req.ID == "" {
		req.ID = randomID("kr")
	}
	if req.Status == "" {
		req.Status = "pending"
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO kill_requests (id, session_id, reason, requested_by, requested_at, expires_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		req.ID, req.SessionID, req.Reason, req.RequestedBy, req.RequestedAt, req.ExpiresAt, req.Status)
	return mapErr(err)
}

func (p *PG) GetKillRequest(ctx context.Context, id string) (*KillRequest, error) {
	return scanKillRequest(p.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM kill_requests WHERE id=$1`, killRequestCols), id))
}

func (p *PG) ListKillRequests(ctx context.Context, status string, page Page) ([]*KillRequest, error) {
	limit, offset := pageArgs(page, 100)
	args := []any{}
	where := ""
	if status != "" {
		args = append(args, status)
		where = fmt.Sprintf(" WHERE status=$%d", len(args))
	}
	args = append(args, limit, offset)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM kill_requests%s ORDER BY requested_at DESC LIMIT $%d OFFSET $%d`,
			killRequestCols, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*KillRequest
	for rows.Next() {
		r, err := scanKillRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) ConfirmKillRequest(ctx context.Context, id, confirmer string, now time.Time) (bool, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE kill_requests SET status='confirmed', confirmed_by=$2, confirmed_at=$3
		WHERE id=$1 AND status='pending' AND expires_at > $3`, id, confirmer, now)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

func (p *PG) ExpireKillRequests(ctx context.Context, now time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx, `
		UPDATE kill_requests SET status='expired' WHERE status='pending' AND expires_at <= $1`, now)
	if err != nil {
		return 0, mapErr(err)
	}
	return int(tag.RowsAffected()), nil
}

const blacklistCols = `id, kind, value, note, expires_at, created_at, created_by`

func (p *PG) AddBlacklist(ctx context.Context, e *BlacklistEntry) error {
	if e.ID == "" {
		e.ID = randomID("blk")
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO blacklist_entries (id, kind, value, note, expires_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		e.ID, e.Kind, e.Value, e.Note, e.ExpiresAt, e.CreatedBy)
	if err != nil {
		return mapErr(err)
	}
	return nil
}

func (p *PG) ListBlacklist(ctx context.Context, page Page) ([]*BlacklistEntry, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM blacklist_entries ORDER BY created_at DESC LIMIT $1 OFFSET $2`, blacklistCols),
		limit, offset)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*BlacklistEntry
	for rows.Next() {
		var e BlacklistEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Value, &e.Note, &e.ExpiresAt, &e.CreatedAt, &e.CreatedBy); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &e)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountBlacklist(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM blacklist_entries`).Scan(&n)
	return n, mapErr(err)
}

func (p *PG) GetBlacklist(ctx context.Context, id string) (*BlacklistEntry, error) {
	var e BlacklistEntry
	err := p.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM blacklist_entries WHERE id=$1`, blacklistCols), id).
		Scan(&e.ID, &e.Kind, &e.Value, &e.Note, &e.ExpiresAt, &e.CreatedAt, &e.CreatedBy)
	if err != nil {
		return nil, mapErr(err)
	}
	return &e, nil
}

func (p *PG) RemoveBlacklist(ctx context.Context, id string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM blacklist_entries WHERE id=$1`, id)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) FindActiveBlacklist(ctx context.Context, kind, value string, now time.Time) (*BlacklistEntry, error) {
	var e BlacklistEntry
	err := p.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM blacklist_entries
		 WHERE kind=$1 AND value=$2 AND (expires_at IS NULL OR expires_at > $3) LIMIT 1`, blacklistCols),
		kind, value, now).
		Scan(&e.ID, &e.Kind, &e.Value, &e.Note, &e.ExpiresAt, &e.CreatedAt, &e.CreatedBy)
	if err != nil {
		return nil, mapErr(err)
	}
	return &e, nil
}

func (p *PG) MatchIPBlacklist(ctx context.Context, ip string, now time.Time) (*BlacklistEntry, error) {
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM blacklist_entries
		 WHERE kind='ip' AND (expires_at IS NULL OR expires_at > $1)`, blacklistCols), now)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	parsed := net.ParseIP(ip)
	for rows.Next() {
		var e BlacklistEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Value, &e.Note, &e.ExpiresAt, &e.CreatedAt, &e.CreatedBy); err != nil {
			return nil, mapErr(err)
		}
		if e.Value == ip {
			return &e, nil
		}
		if parsed != nil && strings.Contains(e.Value, "/") {
			if _, cidr, cerr := net.ParseCIDR(e.Value); cerr == nil && cidr.Contains(parsed) {
				return &e, nil
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}
	return nil, ErrNotFound
}

func (p *PG) RecordAlert(ctx context.Context, a *RiskAlert) error {
	if a.ID == "" {
		a.ID = randomID("alert")
	}
	ts := a.TS
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO risk_alerts (id, kind, subject, detail, ts) VALUES ($1,$2,$3,$4,$5)`,
		a.ID, a.Kind, a.Subject, a.Detail, ts)
	return mapErr(err)
}

func (p *PG) ListAlerts(ctx context.Context, page Page) ([]*RiskAlert, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		`SELECT id, kind, subject, detail, ts FROM risk_alerts ORDER BY ts DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*RiskAlert
	for rows.Next() {
		var a RiskAlert
		if err := rows.Scan(&a.ID, &a.Kind, &a.Subject, &a.Detail, &a.TS); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &a)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountAlerts(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM risk_alerts`).Scan(&n)
	return n, mapErr(err)
}

const versionCols = `id, platform, version, status, rollout_percent, force_update, notes, created_at, updated_at`

func scanVersion(row interface{ Scan(...any) error }) (*ClientVersion, error) {
	var v ClientVersion
	if err := row.Scan(&v.ID, &v.Platform, &v.Version, &v.Status, &v.RolloutPercent, &v.ForceUpdate, &v.Notes, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, mapErr(err)
	}
	return &v, nil
}

func (p *PG) ListVersions(ctx context.Context, page Page) ([]*ClientVersion, error) {
	limit, offset := pageArgs(page, 100)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM client_versions ORDER BY platform ASC, created_at DESC LIMIT $1 OFFSET $2`, versionCols),
		limit, offset)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*ClientVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountVersions(ctx context.Context) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT count(*) FROM client_versions`).Scan(&n)
	return n, mapErr(err)
}

func (p *PG) GetVersion(ctx context.Context, id string) (*ClientVersion, error) {
	return scanVersion(p.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT %s FROM client_versions WHERE id=$1`, versionCols), id))
}

func (p *PG) UpsertVersion(ctx context.Context, v *ClientVersion) error {
	if v.ID == "" {
		v.ID = randomID("ver")
	}
	row := p.pool.QueryRow(ctx, `
		INSERT INTO client_versions (id, platform, version, status, rollout_percent, force_update, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (platform, version) DO UPDATE
		SET status=$4, rollout_percent=$5, force_update=$6, notes=$7, updated_at=now()
		RETURNING id`,
		v.ID, v.Platform, v.Version, v.Status, v.RolloutPercent, v.ForceUpdate, v.Notes)
	if err := row.Scan(&v.ID); err != nil {
		return mapErr(err)
	}
	return nil
}

func (p *PG) UpdateVersionStatus(ctx context.Context, id, status string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE client_versions SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PG) QualityStatsSince(ctx context.Context, from time.Time) (QualityStats, error) {
	var out QualityStats
	var p50, p95 *float64
	var relayPct *float64
	var n int
	err := p.pool.QueryRow(ctx,
		`SELECT count(*),
		        percentile_cont(0.5) WITHIN GROUP (ORDER BY latency_ms),
		        percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms),
		        100.0 * avg(CASE WHEN relay THEN 1.0 ELSE 0.0 END)
		 FROM session_quality_samples WHERE ts >= $1`, from).
		Scan(&n, &p50, &p95, &relayPct)
	if err != nil {
		return out, mapErr(err)
	}
	out.Samples = n
	if n > 0 {
		if p50 != nil {
			out.LatencyP50Ms = int(*p50 + 0.5)
		}
		if p95 != nil {
			out.LatencyP95Ms = int(*p95 + 0.5)
		}
		if relayPct != nil {
			out.RelayPercent = int(*relayPct + 0.5)
		}
	}
	return out, nil
}

func (p *PG) QualityLinkStats(ctx context.Context, from time.Time) (QualityLinkStats, error) {
	var out QualityLinkStats
	err := p.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE link='p2p'),
		       count(*) FILTER (WHERE link='relay'),
		       count(*) FILTER (WHERE target_fps > 0 AND fps >= target_fps),
		       count(*) FILTER (WHERE target_fps > 0)
		FROM session_quality_samples WHERE ts >= $1`, from).
		Scan(&out.P2P, &out.Relay, &out.FpsMet, &out.FpsTotal)
	return out, mapErr(err)
}

func (p *PG) AggregateDeviceQuality(ctx context.Context, deviceID string, from time.Time) ([]DeviceQualityDay, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT to_char(date_trunc('day', q.ts), 'YYYY-MM-DD') AS d,
		        round(avg(q.latency_ms))::int,
		        round(avg(q.loss_permille))::int,
		        round(100.0 * avg(CASE WHEN q.relay THEN 1 ELSE 0 END))::int,
		        count(*)
		 FROM session_quality_samples q
		 JOIN sessions s ON s.id = q.session_id
		 WHERE (s.controller_id = $1 OR s.target_id = $1) AND q.ts >= $2
		 GROUP BY d ORDER BY d ASC`,
		deviceID, from)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []DeviceQualityDay
	for rows.Next() {
		var d DeviceQualityDay
		if err := rows.Scan(&d.Date, &d.AvgLatencyMs, &d.AvgLossPermille, &d.RelayPercent, &d.Samples); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, d)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) ListAuditActions(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT DISTINCT action FROM audit_logs ORDER BY action`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, a)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) ListAuditPage(ctx context.Context, page Page) ([]*AuditLog, error) {
	limit, offset := pageArgs(page, 100)
	args := []any{}
	where := auditWhere(page, &args)
	args = append(args, limit, offset)
	rows, err := p.pool.Query(ctx,
		fmt.Sprintf("SELECT id, actor, role, action, object, before, after, ip, result, ts FROM audit_logs%s ORDER BY ts DESC LIMIT $%d OFFSET $%d", where, len(args)-1, len(args)),
		args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []*AuditLog
	for rows.Next() {
		var a AuditLog
		if err := rows.Scan(&a.ID, &a.Actor, &a.Role, &a.Action, &a.Object, &a.Before, &a.After, &a.IP, &a.Result, &a.TS); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, &a)
	}
	return out, mapErr(rows.Err())
}

func (p *PG) CountAudit(ctx context.Context, page Page) (int, error) {
	args := []any{}
	where := auditWhere(page, &args)
	var n int
	err := p.pool.QueryRow(ctx, "SELECT count(*) FROM audit_logs"+where, args...).Scan(&n)
	return n, mapErr(err)
}
