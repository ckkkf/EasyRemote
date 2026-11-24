// Package session 负责建连请求的生命周期与授权窗口。
//
// 状态迁移只允许通过 Apply(Command) 发生：command 携带目标动作，Service 负责
// 读取当前版本、校验迁移、用 CAS 写回。HTTP/signal/后台不得直接改字段。
package session

import (
	"context"
	"errors"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
	"github.com/easyremote/server/internal/trace"
)

// 会话状态机：requested → authorized → active → closed
const (
	StateRequested  = "requested"
	StateAuthorized = "authorized"
	StateActive     = "active"
	StateClosed     = "closed"
)

// AuthWindow 是授权有效期：窗口内重连不重新弹授权。
const AuthWindow = 12 * time.Hour

// ConsentTimeout 是被控端未响应而自动失败的时间。
const ConsentTimeout = 30 * time.Second

// allowed 是状态迁移白名单。未列出的迁移一律拒绝。
var allowed = map[string]map[string]bool{
	StateRequested:  {StateAuthorized: true, StateClosed: true},
	StateAuthorized: {StateActive: true, StateClosed: true},
	StateActive:     {StateClosed: true},
	StateClosed:     {},
}

func canTransition(from, to string) bool {
	if m, ok := allowed[from]; ok {
		return m[to]
	}
	return false
}

// CommandKind 是会话命令类型。
type CommandKind string

const (
	CommandRequest  CommandKind = "request"
	CommandConsent  CommandKind = "consent"
	CommandReady    CommandKind = "ready"
	CommandActivate CommandKind = "activate"
	CommandClose    CommandKind = "close"

	commandExpireForRequest CommandKind = "expire_for_request"
)

// Command 是一次状态迁移请求。
//
// ExpectedVersion 用于调用方做外部乐观并发；0 表示由 Service 内部 CAS 重试。
type Command struct {
	Kind            CommandKind
	SessionID       string
	ControllerID    string
	TargetID        string
	DeviceID        string
	Accepted        bool
	Reason          string
	ExpectedVersion int64
}

// Service 管理会话。
type Service struct {
	store store.Store
	now   func() time.Time
}

func New(s store.Store) *Service {
	return &Service{store: s, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// Apply 执行一条会话命令。所有状态迁移都必须走这里。
func (s *Service) Apply(ctx context.Context, cmd Command) (*store.Session, error) {
	switch cmd.Kind {
	case CommandRequest:
		return s.request(ctx, cmd.ControllerID, cmd.TargetID)
	case CommandConsent, CommandReady, CommandActivate, CommandClose, commandExpireForRequest:
		return s.applyTransition(ctx, cmd)
	default:
		return nil, errcode.New(errcode.SrvInternal, "unknown session command: "+string(cmd.Kind))
	}
}

// Request 发起一次建连请求。只复用同一对设备尚未失效的会话。
func (s *Service) Request(ctx context.Context, controllerID, targetID string) (*store.Session, error) {
	return s.Apply(ctx, Command{Kind: CommandRequest, ControllerID: controllerID, TargetID: targetID})
}

func (s *Service) request(ctx context.Context, controllerID, targetID string) (*store.Session, error) {
	if controllerID == "" || targetID == "" {
		return nil, errcode.New(errcode.SrvInternal, "missing device id")
	}
	if controllerID == targetID {
		return nil, errcode.New(errcode.ConnSelfConnect, controllerID)
	}
	const maxRequestAttempts = 5
	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		candidate := &store.Session{
			ControllerID: controllerID, TargetID: targetID,
			State: StateRequested, StartedAt: s.now(),
		}
		// 复用对象由设备对的原子写入返回，不依赖会话列表的页大小或排序。
		live, created, err := s.store.CreateSessionExclusive(ctx, candidate)
		if err != nil {
			return nil, errcode.New(errcode.SrvInternal, err.Error())
		}
		if created || requestExpiryReason(live, s.now()) == "" {
			return live, nil
		}
		current, err := s.Apply(ctx, Command{Kind: commandExpireForRequest, SessionID: live.ID})
		if err != nil {
			return nil, err
		}
		if current.State != StateClosed {
			return current, nil
		}
	}
	return nil, errcode.New(errcode.SrvInternal, "session busy: too many concurrent requests")
}

func requestExpiryReason(sess *store.Session, now time.Time) string {
	switch sess.State {
	case StateRequested:
		if now.Sub(sess.StartedAt) > ConsentTimeout {
			return "request_expired"
		}
	case StateAuthorized:
		if authorizationExpired(sess, now) {
			return "auth_window_exceeded"
		}
	}
	return ""
}

func authorizationExpired(sess *store.Session, now time.Time) bool {
	return sess.AuthExpireAt.IsZero() || !now.Before(sess.AuthExpireAt)
}

// RelayAuthorizationDeadline 校验已完成设备验权的会话快照是否可初领或换领票据。
// active 只证明数据面已经就绪，不延长被控端给出的授权窗口。
func (s *Service) RelayAuthorizationDeadline(sess *store.Session) (time.Time, error) {
	if sess.State != StateAuthorized && sess.State != StateActive {
		return time.Time{}, errcode.New(errcode.SessionAuthWindowExceeded, "session not authorized")
	}
	if authorizationExpired(sess, s.now()) {
		return time.Time{}, errcode.New(errcode.SessionAuthWindowExceeded, "session authorization expired")
	}
	return sess.AuthExpireAt, nil
}

// Consent 处理被控端授权结果（兼容入口，内部走 Apply）。
func (s *Service) Consent(ctx context.Context, sessionID string, accepted bool, reason string) (*store.Session, error) {
	return s.Apply(ctx, Command{
		Kind:      CommandConsent,
		SessionID: sessionID,
		Accepted:  accepted,
		Reason:    reason,
	})
}

func (s *Service) applyTransition(ctx context.Context, cmd Command) (*store.Session, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		sess, err := s.get(ctx, cmd.SessionID)
		if err != nil {
			return nil, err
		}
		if cmd.ExpectedVersion > 0 && sess.Version != cmd.ExpectedVersion {
			return nil, errcode.New(errcode.SrvInternal, "session version conflict")
		}
		next, changed, finalErr := transition(sess, cmd, s.now())
		if !changed {
			return sess, finalErr
		}
		// 领域事件与状态迁移同事务写入 outbox；重复迁移由 (session_id, version) 去重。
		ev := &store.SessionEvent{
			SessionID:  next.ID,
			FromDevice: next.ControllerID,
			ToDevice:   next.TargetID,
			OldState:   sess.State,
			NewState:   next.State,
			Reason:     next.Reason,
			TraceID:    trace.From(ctx),
			Version:    sess.Version + 1,
			At:         s.now(),
		}
		if err := s.store.UpdateSessionCASWithEvent(ctx, next, sess.Version, ev); err != nil {
			if errors.Is(err, store.ErrVersionConflict) {
				continue
			}
			return nil, errcode.New(errcode.SrvInternal, err.Error())
		}
		return next, finalErr
	}
	return nil, errcode.New(errcode.SrvInternal, "session busy: too many concurrent transitions")
}

// transition 根据当前状态计算下一状态；返回 changed=false 表示幂等 no-op。
func transition(sess *store.Session, cmd Command, now time.Time) (*store.Session, bool, error) {
	next := *sess
	switch cmd.Kind {
	case CommandConsent:
		if sess.State == StateRequested {
			// ER-905：拒绝/超时都是**成功完成的本机决策**，权威状态落 closed，
			// 不返回业务错误。契约 GW-07 的 response 是 ConsentResult，
			// errors 只允许 CONN_DEVICE_NOT_FOUND / ADMIN_FORBIDDEN；
			// 之前这里返回 200006/200007 让 handler 写成 4xx，
			// 客户端因此拿不到 closed，主控一直卡在等待。
			if now.Sub(sess.StartedAt) > ConsentTimeout {
				next.State = StateClosed
				next.Reason = "consent_timeout"
				next.CloseKind = string(ClassifyClose(next.Reason))
				next.EndedAt = now
				return &next, true, nil
			}
			if cmd.Accepted {
				next.State = StateAuthorized
				next.AuthExpireAt = now.Add(AuthWindow)
				return &next, true, nil
			}
			next.State = StateClosed
			next.Reason = cmd.Reason
			if next.Reason == "" {
				next.Reason = "user_rejected"
			}
			next.CloseKind = string(ClassifyClose(next.Reason))
			next.EndedAt = now
			return &next, true, nil
		}
		if cmd.Accepted {
			// 重复接受/已授权：幂等成功。
			if sess.State == StateAuthorized || sess.State == StateActive {
				return sess, false, nil
			}
			return nil, false, errcode.New(errcode.SessionNotFound, "not requestable: "+sess.State)
		}
		if sess.State == StateClosed {
			return sess, false, nil
		}
		if !canTransition(sess.State, StateClosed) {
			return nil, false, errcode.New(errcode.SessionNotFound, "cannot close: "+sess.State)
		}
		next.State = StateClosed
		next.Reason = cmd.Reason
		next.CloseKind = string(ClassifyClose(next.Reason))
		next.EndedAt = now
		return &next, true, errcode.New(errcode.ConnConsentRejected, sess.ID)

	case CommandReady:
		if cmd.DeviceID == "" {
			return nil, false, errcode.New(errcode.SrvInternal, "missing device id")
		}
		if sess.State == StateClosed {
			return nil, false, errcode.New(errcode.SessionNotFound, "session closed")
		}
		if sess.State != StateAuthorized && sess.State != StateActive {
			return nil, false, errcode.New(errcode.SessionNotFound, "not authorized: "+sess.State)
		}
		// 授权窗口过期后不能再 ready，必须收口为 closed。
		if sess.State == StateAuthorized && authorizationExpired(sess, now) {
			next.State = StateClosed
			// 先写 reason 再分类：否则 ClassifyClose 会按旧值/空串算成 unknown，
			// 授权窗口到期就会被后台统计成「异常原因未知」。
			next.Reason = "auth_window_exceeded"
			next.CloseKind = string(ClassifyClose(next.Reason))
			next.EndedAt = now
			return &next, true, errcode.New(errcode.SessionAuthWindowExceeded, sess.ID)
		}
		changed := false
		switch cmd.DeviceID {
		case sess.ControllerID:
			if !next.ReadyController {
				next.ReadyController = true
				changed = true
			}
		case sess.TargetID:
			if !next.ReadyTarget {
				next.ReadyTarget = true
				changed = true
			}
		default:
			return nil, false, errcode.New(errcode.AdminForbidden, "not a session party")
		}
		// 双方都 ready 且已授权，才允许进入 active。
		if next.State == StateAuthorized && next.ReadyController && next.ReadyTarget {
			next.State = StateActive
			changed = true
		}
		if !changed {
			return sess, false, nil
		}
		return &next, true, nil

	case CommandActivate:
		if sess.State == StateActive {
			return sess, false, nil
		}
		if sess.State != StateAuthorized {
			return nil, false, errcode.New(errcode.SessionNotFound, "not authorized: "+sess.State)
		}
		if !sess.ReadyController || !sess.ReadyTarget {
			return nil, false, errcode.New(errcode.SessionNotFound, "peers not ready")
		}
		if authorizationExpired(sess, now) {
			next.State = StateClosed
			// 先写 reason 再分类：否则 ClassifyClose 会按旧值/空串算成 unknown，
			// 授权窗口到期就会被后台统计成「异常原因未知」。
			next.Reason = "auth_window_exceeded"
			next.CloseKind = string(ClassifyClose(next.Reason))
			next.EndedAt = now
			return &next, true, errcode.New(errcode.SessionAuthWindowExceeded, sess.ID)
		}
		if !canTransition(sess.State, StateActive) {
			return nil, false, errcode.New(errcode.SessionNotFound, "bad transition")
		}
		next.State = StateActive
		return &next, true, nil

	case CommandClose, commandExpireForRequest:
		if sess.State == StateClosed {
			return sess, false, nil
		}
		if cmd.Kind == commandExpireForRequest {
			// CAS 冲突后重新检查，避免根据旧快照关闭已经就绪的会话。
			cmd.Reason = requestExpiryReason(sess, now)
			if cmd.Reason == "" {
				return sess, false, nil
			}
		}
		if !canTransition(sess.State, StateClosed) {
			return nil, false, errcode.New(errcode.SessionNotFound, "cannot close: "+sess.State)
		}
		next.State = StateClosed
		next.Reason = cmd.Reason
		next.CloseKind = string(ClassifyClose(next.Reason))
		next.EndedAt = now
		return &next, true, nil
	}
	return nil, false, errcode.New(errcode.SrvInternal, "unknown transition")
}

// Ready 记录某一端数据面就绪；两端都就绪后由状态机进入 active。
func (s *Service) Ready(ctx context.Context, sessionID, deviceID string) (*store.Session, error) {
	return s.Apply(ctx, Command{Kind: CommandReady, SessionID: sessionID, DeviceID: deviceID})
}

// Activate 标记会话进入 active（要求双方 ready）。
func (s *Service) Activate(ctx context.Context, sessionID string) (*store.Session, error) {
	return s.Apply(ctx, Command{Kind: CommandActivate, SessionID: sessionID})
}

// Kill 关闭会话（后台强制中断或任一端主动结束）。
func (s *Service) Kill(ctx context.Context, sessionID, reason string) (*store.Session, error) {
	return s.Apply(ctx, Command{Kind: CommandClose, SessionID: sessionID, Reason: reason})
}

// Close 由任一端主动结束。
func (s *Service) Close(ctx context.Context, sessionID, reason string) (*store.Session, error) {
	return s.Kill(ctx, sessionID, reason)
}

// Get 查询会话。
func (s *Service) Get(ctx context.Context, sessionID string) (*store.Session, error) {
	return s.get(ctx, sessionID)
}

func (s *Service) get(ctx context.Context, sessionID string) (*store.Session, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errcode.New(errcode.SessionNotFound, sessionID)
	} else if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return sess, nil
}

// List 列出会话，state 为空表示全部。
func (s *Service) List(ctx context.Context, state string) ([]*store.Session, error) {
	return s.store.ListSessions(ctx, state, store.Page{Limit: 200})
}
