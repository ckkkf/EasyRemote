// Package store 定义存储接口与领域类型。
//
// 设计约束（审查项 S10）：
//   - 所有方法带 context.Context，允许设置查询超时与取消；
//   - 读方法返回 error，"不存在" 用 ErrNotFound 表达，避免把 DB 故障当成不存在；
//   - 列表方法显式提供，不依赖类型断言；
//   - 提供原子原语（VerifyAndConsume），让 PG 实现能用单条 UPDATE 保证并发正确。
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound 表示记录不存在。与数据库故障严格区分。
var ErrNotFound = errors.New("store: not found")

// ErrConflict 表示唯一键冲突。
var ErrConflict = errors.New("store: conflict")

// Account 是账号。手机号不存明文，只存哈希与密文。
type Account struct {
	ID        string    `json:"id"`
	PhoneHash string    `json:"-"`
	PhoneEnc  string    `json:"-"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Device 是注册设备。
type Device struct {
	ID         string    `json:"id"`
	DeviceCode string    `json:"device_code"`
	AccountID  string    `json:"account_id"`
	Platform   string    `json:"platform"`
	LastSeenAt time.Time `json:"last_seen_at"`
	Online     bool      `json:"online"`
	TokenHash  string    `json:"-"`
	// PasswordHash 是「临时密码」的哈希：长期有效、校验不消费、被控端可查看明文后复制。
	// 与一次性验证码（VerifyCode）并存：老客户端仍能走验证码，新客户端用密码。
	PasswordHash string `json:"-"`
	// IdentityPubKey 是设备长期身份公钥（Ed25519 hex），端到端握手验签用。
	IdentityPubKey string `json:"identity_pubkey,omitempty"`
}

// VerifyCode 是被控端的一次性验证码。
type VerifyCode struct {
	DeviceCode string
	CodeHash   string
	ExpiresAt  time.Time
	Attempts   int
	LockedTill time.Time
}

// TrustEdge 表示主控对被控的信任关系。
type TrustEdge struct {
	ControllerID string
	TargetID     string
	ExpiresAt    time.Time
}

// Session 是一次远程会话。
type Session struct {
	ID           string    `json:"id"`
	ControllerID string    `json:"controller_id"`
	TargetID     string    `json:"target_id"`
	State        string    `json:"state"`
	Reason       string    `json:"reason,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	AuthExpireAt time.Time `json:"auth_expire_at,omitempty"`
}

// FileMeta 是一次文件传输的元数据。字节内容落盘，元数据入库：
// 元数据可查、可审计、可跨实例共享，字节走文件系统不占数据库。
type FileMeta struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	DeviceID  string    `json:"device_id"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
	// StoragePath 是落盘的绝对路径，只在下发字节时使用，绝不回给调用方。
	StoragePath string `json:"-"`
}

// AssistCode 是一次性协助码。
//
// 语义：被控设备自己生成码（证明「我允许谁进来」），
// 主控用码换取一个 requested 会话，跳过验证码环节。
// 码只能成功兑换一次，兑换由存储层原子完成，避免并发双花。
type AssistCode struct {
	Code       string
	AccountID  string
	TargetID   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	RedeemedAt time.Time
}

// Redeemed 判断该码是否已被兑换。
func (c *AssistCode) Redeemed() bool { return !c.RedeemedAt.IsZero() }

// AuditLog 是后台操作审计，append-only。
type AuditLog struct {
	ID     string    `json:"id"`
	Actor  string    `json:"actor"`
	Role   string    `json:"role"`
	Action string    `json:"action"`
	Object string    `json:"object"`
	Before string    `json:"before,omitempty"`
	After  string    `json:"after,omitempty"`
	IP     string    `json:"ip"`
	Result string    `json:"result"`
	TS     time.Time `json:"ts"`
}

// AdminUser 是后台账号。
type AdminUser struct {
	ID           string
	Username     string
	PasswordHash string
	TOTPSecret   string
	Role         string
	Status       string
}

// Page 是列表分页参数。Limit<=0 表示使用默认值。
type Page struct {
	Limit  int
	Offset int
}

// VerifyOutcome 是验证码校验结果。
type VerifyOutcome int

const (
	VerifyOK VerifyOutcome = iota
	VerifyInvalid
	VerifyExpired
	VerifyLocked
)

// VerifyResult 携带结果与剩余尝试次数。
type VerifyResult struct {
	Outcome  VerifyOutcome
	Attempts int
	LockedTo time.Time
}

// Store 是存储接口。内存实现用于测试，PG 实现用于生产，
// 两者必须通过同一套契约测试（server/internal/store/storetest）。
type Store interface {
	// accounts
	CreateAccount(ctx context.Context, a *Account) error
	GetAccountByPhoneHash(ctx context.Context, hash string) (*Account, error)
	GetAccount(ctx context.Context, id string) (*Account, error)
	UpdateAccountStatus(ctx context.Context, id, status string) error
	ListAccounts(ctx context.Context, p Page) ([]*Account, error)

	// devices
	CreateDevice(ctx context.Context, d *Device) error
	GetDeviceByCode(ctx context.Context, code string) (*Device, error)
	GetDevice(ctx context.Context, id string) (*Device, error)
	ListDevicesByAccount(ctx context.Context, accountID string, p Page) ([]*Device, error)
	ListDevices(ctx context.Context, p Page) ([]*Device, error)
	UpdateDeviceSeen(ctx context.Context, id string, at time.Time, online bool) error
	// UpdateDevicePlatform 刷新设备平台名（幂等绑定复用同一行时用）。
	UpdateDevicePlatform(ctx context.Context, id, platform string) error
	UpdateDeviceToken(ctx context.Context, id, tokenHash string) error
	UpdateDeviceIdentity(ctx context.Context, id, pubkey string) error
	UpdateDevicePassword(ctx context.Context, id, passwordHash string) error
	GetDeviceIdentity(ctx context.Context, id string) (string, error)

	// verify codes
	UpsertVerifyCode(ctx context.Context, v *VerifyCode) error
	GetVerifyCode(ctx context.Context, deviceCode string) (*VerifyCode, error)
	VerifyAndConsume(ctx context.Context, deviceCode, codeHash string, now time.Time, maxAttempts int, lockFor time.Duration) (VerifyResult, error)

	// trust
	UpsertTrust(ctx context.Context, t *TrustEdge) error
	HasTrust(ctx context.Context, controllerID, targetID string, now time.Time) (bool, error)

	// sessions
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, s *Session) error
	ListSessions(ctx context.Context, state string, p Page) ([]*Session, error)

	// files
	CreateFile(ctx context.Context, f *FileMeta) error
	GetFile(ctx context.Context, id string) (*FileMeta, error)
	ListFilesByAccount(ctx context.Context, accountID string, p Page) ([]*FileMeta, error)
	// DeleteFile 删除元数据；记录不存在返回 ErrNotFound（调用方按幂等处理）。
	DeleteFile(ctx context.Context, id string) error

	// assist codes
	PutAssistCode(ctx context.Context, c *AssistCode) error
	GetAssistCode(ctx context.Context, code string) (*AssistCode, error)
	// RedeemAssistCode 原子消费协助码：
	// 只有「存在 + 未过期 + 未兑换」才会成功，返回 true。
	// 其余情况返回 false（调用方再读一次区分原因）。
	RedeemAssistCode(ctx context.Context, code string, now time.Time) (bool, error)

	// app config（键值，键的下发白名单与取值校验由上层负责）
	PutConfig(ctx context.Context, key, value string) error
	ListConfig(ctx context.Context) (map[string]string, error)

	// admin
	GetAdminByUsername(ctx context.Context, username string) (*AdminUser, error)
	PutAdmin(ctx context.Context, a *AdminUser) error
	AppendAudit(ctx context.Context, a *AuditLog) error
	ListAudit(ctx context.Context, limit int) ([]*AuditLog, error)

	// Ping 用于健康检查。
	Ping(ctx context.Context) error
	Close() error
}
