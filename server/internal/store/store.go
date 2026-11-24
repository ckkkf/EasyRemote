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

// ErrVersionConflict 表示会话已被其它 command 修改（CAS 失败）。
var ErrVersionConflict = errors.New("store: version conflict")

// ErrConflict 表示唯一键冲突。
var ErrConflict = errors.New("store: conflict")

// QualitySample 是一秒钟的会话质量采样（客户端上报，服务端只存不造）。
type QualitySample struct {
	SessionID    string
	TS           time.Time
	LatencyMs    int
	LossPermille int
	Fps          int
	Tier         string
	Relay        bool
	// Link 是客户端上报的实际链路（p2p / relay / 空=未上报）。
	// 服务端不根据"有没有签中继票据"反推 P2P——那是猜测，不是观测。
	Link string
	// TargetFps 是本次会话的目标帧率（0=未上报），用于计算帧率达成率。
	TargetFps int
}

// TrendPoint 是一个时间桶的聚合值（ADM-API-02f3）。
type TrendPoint struct {
	BucketStart     time.Time
	SessionsStarted int
	ConnectAttempts int
	ConnectActive   int
}

// IsAbnormalCloseKind 判断结束分类是否算"异常中断"。
// 分类本身由 session.ClassifyClose 决定（唯一来源）；这里只做集合判断，避免 import 环。
func IsAbnormalCloseKind(kind string) bool {
	switch kind {
	case "user_hangup", "admin_kill":
		return false
	default:
		// 空分类（历史数据）按异常处理：宁可多算，不掩盖问题。
		return true
	}
}

// SessionError 是会话级错误事件（ADM-API-03h）。
type SessionError struct {
	ID        string
	SessionID string
	Code      int
	Name      string
	Detail    string
	Source    string
	TS        time.Time
}

// SessionSummary 是会话级质量摘要（ADM-API-02f7）。
type SessionSummary struct {
	Samples      int
	AvgLatencyMs int
	P2PCount     int
	RelayCount   int
}

// HealthEvent 是客户端健康事件（ADM-API-02f5）。Kind: crash / reconnect_attempt / reconnect_success。
type HealthEvent struct {
	ID        string
	SessionID string
	DeviceID  string
	Kind      string
	TS        time.Time
}

// HealthStats 是窗口内的客户端健康统计。
type HealthStats struct {
	Crashes           int
	ReconnectAttempts int
	ReconnectSuccess  int
}

// AccountNote 是账号备注（ADM-API-03c）。
type AccountNote struct {
	AccountID string
	Note      string
	UpdatedBy string
	UpdatedAt time.Time
}

// ConnectAttempt 是一次建连尝试（ADM-API-02f2）。
// Link 只记录服务端能观察到的事实：unknown / relay；p2p 需要客户端上报，服务端不猜。
type ConnectAttempt struct {
	SessionID    string
	AccountID    string
	ControllerID string
	TargetID     string
	Outcome      string
	Link         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// KillRequest 是强制中断的双人确认请求（ADM-API-07b1）。
type KillRequest struct {
	ID          string
	SessionID   string
	Reason      string
	RequestedBy string
	RequestedAt time.Time
	ExpiresAt   time.Time
	Status      string // pending / confirmed / expired
	ConfirmedBy string
	ConfirmedAt time.Time
}

// BlacklistEntry 是风控黑名单条目（A10，ADM-API-06c）。Kind: ip / device / account。
type BlacklistEntry struct {
	ID        string
	Kind      string
	Value     string
	Note      string
	ExpiresAt *time.Time
	CreatedAt time.Time
	CreatedBy string
}

// RiskAlert 是异常连接告警（A10）：目前只有 blacklist_block 一种来源。
type RiskAlert struct {
	ID      string
	Kind    string
	Subject string
	Detail  string
	TS      time.Time
}

// ClientVersion 是客户端版本与灰度策略（A11，ADM-API-06b）。
type ClientVersion struct {
	ID             string
	Platform       string
	Version        string
	Status         string // draft / rolling / full / retired
	RolloutPercent int
	ForceUpdate    bool
	Notes          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// QualityStats 是窗口内的全局质量统计（仪表盘质量卡，ADM-API-02e）。
type QualityStats struct {
	Samples      int
	LatencyP50Ms int
	LatencyP95Ms int
	RelayPercent int
}

// QualityLinkStats 是链路与帧率维度的聚合（ADM-API-02f4）。
// 只有"上报过 link 的采样"才进 P2P 分母；只有"上报过 target_fps"的采样才进帧率分母。
type QualityLinkStats struct {
	P2P      int
	Relay    int
	FpsMet   int
	FpsTotal int
}

// DeviceQualityDay 是某设备某天的质量聚合（ADM-API-02d）。
type DeviceQualityDay struct {
	Date            string // YYYY-MM-DD（UTC）
	AvgLatencyMs    int
	AvgLossPermille int
	RelayPercent    int
	Samples         int
}

// Account 是账号。手机号不存明文，只存哈希与密文。
type Account struct {
	// Nickname 是运营可读的账号昵称（ADM-API-03b）；为空表示未设置。
	Nickname string `json:"nickname"`
	// LastLoginAt 是最近一次成功登录时间；零值表示从未登录（历史数据）。
	LastLoginAt time.Time `json:"last_login_at"`

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
	// 以下为客户端上报的设备元信息（ADM-API-04a）；空串表示尚未上报。
	DeviceName    string `json:"device_name"`
	OSVersion     string `json:"os_version"`
	ClientVersion string `json:"client_version"`
	NATType       string `json:"nat_type"`
	// CreatedAt 是设备绑定时间（ADM-API-02f6），用于"24h 新增设备"。
	CreatedAt time.Time `json:"created_at"`
	// 风险标记（ADM-API-04c）：人工标记，列表可见；解除时清空原因。
	RiskFlag   bool   `json:"risk_flag"`
	RiskReason string `json:"risk_reason"`
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
	ID           string `json:"id"`
	ControllerID string `json:"controller_id"`
	TargetID     string `json:"target_id"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
	// CloseKind 是结束原因的归一分类（ADM-API-03f2a）；空表示历史数据未分类。
	CloseKind    string    `json:"close_kind,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	AuthExpireAt time.Time `json:"auth_expire_at,omitempty"`
	// Version 是乐观并发控制版本；每次成功状态迁移 +1。
	Version int64 `json:"version"`
	// ReadyController/ReadyTarget 由数据面握手完成事件置位；
	// 只有双方都 ready 且已授权，状态机才允许进入 active。
	ReadyController bool `json:"ready_controller"`
	ReadyTarget     bool `json:"ready_target"`
}

// SessionEvent 是会话状态迁移的领域事件（outbox）。
// (SessionID, Version) 唯一：同一迁移重复写入必须被忽略（幂等）。
type SessionEvent struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id"`
	FromDevice string    `json:"from"`
	ToDevice   string    `json:"to"`
	OldState   string    `json:"old_state"`
	NewState   string    `json:"new_state"`
	Reason     string    `json:"reason,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	Version    int64     `json:"version"`
	At         time.Time `json:"at"`
	Delivered  bool      `json:"delivered"`
	Attempts   int       `json:"attempts"`
	LastError  string    `json:"last_error,omitempty"`
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
	// Query 是模糊搜索（各实现在白名单字段内匹配）。
	Query string
	// Sort 是字段名白名单；空表示默认排序。
	Sort string
	// Desc 为 true 时倒序。
	Desc bool
	// Status 是精确状态过滤（账号/会话等；空表示全部）。
	Status string
	// Platform 是设备平台精确过滤。
	Platform string
	// Online 是设备在线状态精确过滤（nil 表示不过滤）。
	Online *bool
	// From/To 是时间范围过滤（零值表示不限）。
	From time.Time
	To   time.Time
	// Actor/Object 是审计的精确过滤（空表示不限）。
	Actor  string
	Object string
	// AccountID/DeviceID 是会话过滤（ADM-API-03f1）：账号按其名下设备参与过滤。
	AccountID string
	DeviceID  string
	// CloseKind / Abnormal / Link 是会话筛选（ADM-API-03f2）：
	// Abnormal=true 表示"异常结束"（结束分类不属于 user_hangup/admin_kill）。
	CloseKind string
	Abnormal  *bool
	Link      string
	// Action/Result 是审计精确过滤。
	Action string
	Result string
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
	// UpdateAccountPhoneEnc 回填手机号密文（历史账号只有哈希，后台脱敏拿不到号码）。
	UpdateAccountPhoneEnc(ctx context.Context, id, phoneEnc string) error
	// TouchAccountLogin 记录最近一次成功登录时间（ADM-API-03b）。
	TouchAccountLogin(ctx context.Context, id string, at time.Time) error
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
	// DeleteDevice 解绑设备（删除设备行）；不存在的设备返回 ErrNotFound。
	DeleteDevice(ctx context.Context, id string) error
	// SetDeviceRisk 标记/解除设备风险；解除时 reason 传空。
	SetDeviceRisk(ctx context.Context, id string, flag bool, reason string) error
	// UpdateDeviceMeta 保存客户端上报的设备元信息；空字符串表示本次不上报该字段。
	UpdateDeviceMeta(ctx context.Context, id, deviceName, osVersion, clientVersion, natType string) error
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
	// CreateSessionExclusive 保证同一对设备最多一个未关闭会话。
	// created=false 时返回已有会话；并发首次请求由存储层串行化。
	CreateSessionExclusive(ctx context.Context, s *Session) (existing *Session, created bool, err error)
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, s *Session) error
	// UpdateSessionCAS 仅当存储中的 version == expected 时写入，并把 version+1。
	// 冲突返回 ErrVersionConflict，调用方必须重新读取并重放 command。
	UpdateSessionCAS(ctx context.Context, s *Session, expected int64) error
	// UpdateSessionCASWithEvent 在同一事务里完成 CAS 迁移 + 写入 outbox 事件。
	UpdateSessionCASWithEvent(ctx context.Context, s *Session, expected int64, ev *SessionEvent) error
	// ListSessionEvents 按 outbox 投递状态读取事件（delivered=false 即待投递）。
	ListSessionEvents(ctx context.Context, delivered bool, limit int) ([]*SessionEvent, error)
	// Count* 返回与对应 List 相同过滤条件下的总数（用于真实分页）。
	CountAccounts(ctx context.Context, p Page) (int, error)
	CountDevices(ctx context.Context, p Page) (int, error)
	// DeviceCountsByAccount 一次查出多个账号的设备数（后台列表列，避免 N+1）。
	DeviceCountsByAccount(ctx context.Context, accountIDs []string) (map[string]int, error)
	CountSessions(ctx context.Context, state string, p Page) (int, error)
	CountAudit(ctx context.Context, p Page) (int, error)
	// ListAuditActions 返回审计里出现过的动作（去重排序），供筛选下拉使用。
	ListAuditActions(ctx context.Context) ([]string, error)
	ListAuditPage(ctx context.Context, p Page) ([]*AuditLog, error)
	MarkSessionEventDelivered(ctx context.Context, id string) error
	MarkSessionEventFailed(ctx context.Context, id, reason string) error
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
	// 配置作用域（ADM-API-05c1）：platform="" 为全局。
	ListConfigOverrides(ctx context.Context, platform, version string) (map[string]string, error)
	SetConfigOverride(ctx context.Context, key, platform, version, value string) error

	// admin
	GetAdminByUsername(ctx context.Context, username string) (*AdminUser, error)
	GetAdmin(ctx context.Context, id string) (*AdminUser, error)
	PutAdmin(ctx context.Context, a *AdminUser) error
	// 管理端 A14（ADM-API-06a）：列出管理员并支持改角色/启用禁用。
	ListAdmins(ctx context.Context, p Page) ([]*AdminUser, error)
	CountAdmins(ctx context.Context) (int, error)
	UpdateAdminRole(ctx context.Context, id, role string) error
	UpdateAdminStatus(ctx context.Context, id, status string) error
	AppendAudit(ctx context.Context, a *AuditLog) error

	// 会话质量采样（ADM-API-02c）：只追加；同一会话同一秒幂等覆盖。
	AppendQualitySamples(ctx context.Context, samples []QualitySample) error
	ListQualitySamples(ctx context.Context, sessionID string, limit int) ([]QualitySample, error)
	// AggregateDeviceQuality 按天聚合某设备参与的会话质量（设备 30 天曲线）。
	AggregateDeviceQuality(ctx context.Context, deviceID string, from time.Time) ([]DeviceQualityDay, error)
	// QualityStatsSince 计算窗口内的延迟 P50/P95、中继占比与采样数（仪表盘质量卡）。
	QualityStatsSince(ctx context.Context, from time.Time) (QualityStats, error)
	// QualityLinkStats 统计客户端上报的链路类型与帧率达成情况（ADM-API-02f4）。
	QualityLinkStats(ctx context.Context, from time.Time) (QualityLinkStats, error)

	// SessionSummaries 批量返回会话的质量摘要（ADM-API-02f7）。
	SessionSummaries(ctx context.Context, sessionIDs []string) (map[string]SessionSummary, error)

	// CountDevicesSince 统计窗口内新绑定的设备数（ADM-API-02f6）。
	CountDevicesSince(ctx context.Context, from time.Time) (int, error)

	// 会话级错误事件（ADM-API-03h）。
	RecordSessionError(ctx context.Context, e *SessionError) error
	ListSessionErrors(ctx context.Context, sessionID string, limit int) ([]*SessionError, error)

	// 客户端健康事件（ADM-API-02f5）。
	RecordHealthEvent(ctx context.Context, e *HealthEvent) error
	HealthStatsSince(ctx context.Context, from time.Time) (HealthStats, error)

	// 账号备注（ADM-API-03c）：空备注表示没写过。
	GetAccountNote(ctx context.Context, accountID string) (*AccountNote, error)
	UpsertAccountNote(ctx context.Context, n *AccountNote) error
	// ListSessionsByAccount 返回该账号名下设备参与的会话（服务端过滤，ADM-API-03c）。
	ListSessionsByAccount(ctx context.Context, accountID string, limit int) ([]*Session, error)

	// 建连尝试埋点（ADM-API-02f2）：requested 记一次尝试，进入 active 记成功。
	CreateConnectAttempt(ctx context.Context, a *ConnectAttempt) error
	MarkConnectAttemptActive(ctx context.Context, sessionID string) error
	MarkConnectAttemptRelay(ctx context.Context, sessionID string) error
	ConnectAttemptStats(ctx context.Context, from time.Time) (total int, active int, err error)
	// TrendBuckets 按固定桶宽聚合会话与建连（桶按 UTC 对齐）。
	TrendBuckets(ctx context.Context, from time.Time, bucket time.Duration) ([]TrendPoint, error)

	// 强制中断双人确认（ADM-API-07b1）。
	CreateKillRequest(ctx context.Context, req *KillRequest) error
	GetKillRequest(ctx context.Context, id string) (*KillRequest, error)
	ListKillRequests(ctx context.Context, status string, p Page) ([]*KillRequest, error)
	// ConfirmKillRequest 用 CAS 把 pending 改成 confirmed；只有未过期的 pending 能成功。
	ConfirmKillRequest(ctx context.Context, id, confirmer string, now time.Time) (bool, error)
	ExpireKillRequests(ctx context.Context, now time.Time) (int, error)

	// 风控黑名单与告警（ADM-API-06c）。
	AddBlacklist(ctx context.Context, e *BlacklistEntry) error
	ListBlacklist(ctx context.Context, p Page) ([]*BlacklistEntry, error)
	CountBlacklist(ctx context.Context) (int, error)
	GetBlacklist(ctx context.Context, id string) (*BlacklistEntry, error)
	RemoveBlacklist(ctx context.Context, id string) error
	// FindActiveBlacklist 返回指定 kind+value 下未过期的条目（无则 ErrNotFound）。
	FindActiveBlacklist(ctx context.Context, kind, value string, now time.Time) (*BlacklistEntry, error)
	// MatchIPBlacklist 支持 CIDR（ip 类条目既可以是单 IP，也可以是网段）。
	MatchIPBlacklist(ctx context.Context, ip string, now time.Time) (*BlacklistEntry, error)
	RecordAlert(ctx context.Context, a *RiskAlert) error
	ListAlerts(ctx context.Context, p Page) ([]*RiskAlert, error)
	CountAlerts(ctx context.Context) (int, error)

	// 客户端版本与灰度（ADM-API-06b）：按 platform+version 唯一。
	ListVersions(ctx context.Context, p Page) ([]*ClientVersion, error)
	CountVersions(ctx context.Context) (int, error)
	GetVersion(ctx context.Context, id string) (*ClientVersion, error)
	UpsertVersion(ctx context.Context, v *ClientVersion) error
	UpdateVersionStatus(ctx context.Context, id, status string) error
	ListAudit(ctx context.Context, limit int) ([]*AuditLog, error)
	// 旧 ListAudit 保留；新代码使用 ListAuditPage + CountAudit。

	// Ping 用于健康检查。
	Ping(ctx context.Context) error
	Close() error
}
