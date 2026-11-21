// Package session 负责建连请求的生命周期与授权窗口。
package session

import (
	"context"
	"errors"
	"time"

	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/store"
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

// canTransition 判断迁移是否合法。
func canTransition(from, to string) bool {
	if m, ok := allowed[from]; ok {
		return m[to]
	}
	return false
}

// Service 管理会话。
type Service struct {
	store store.Store
	now   func() time.Time
}

// New 创建会话服务。
func New(s store.Store) *Service {
	return &Service{store: s, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock 仅测试使用。
func (s *Service) SetClock(f func() time.Time) { s.now = f }

// Request 发起一次建连请求。同一对设备已有未关闭会话时返回既有会话（幂等）。
func (s *Service) Request(ctx context.Context, controllerID, targetID string) (*store.Session, error) {
	if controllerID == "" || targetID == "" {
		return nil, errcode.New(errcode.SrvInternal, "missing device id")
	}
	if controllerID == targetID {
		return nil, errcode.New(errcode.ConnSelfConnect, controllerID)
	}
	existing, err := s.store.ListSessions(ctx, "", store.Page{Limit: 500})
	if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	for _, sess := range existing {
		if sess.ControllerID == controllerID && sess.TargetID == targetID &&
			(sess.State == StateRequested || sess.State == StateAuthorized || sess.State == StateActive) {
			return sess, nil
		}
	}
	now := s.now()
	sess := &store.Session{ControllerID: controllerID, TargetID: targetID, State: StateRequested, StartedAt: now}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return sess, nil
}

// Consent 处理被控端授权结果。授权成功即开启 12 小时窗口。
func (s *Service) Consent(ctx context.Context, sessionID string, accepted bool, reason string) (*store.Session, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errcode.New(errcode.SessionNotFound, sessionID)
	} else if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	now := s.now()

	if sess.State == StateRequested && now.Sub(sess.StartedAt) > ConsentTimeout {
		if canTransition(sess.State, StateClosed) {
			sess.State = StateClosed
			sess.Reason = "consent_timeout"
			sess.EndedAt = now
			if err := s.store.UpdateSession(ctx, sess); err != nil {
				return nil, errcode.New(errcode.SrvInternal, err.Error())
			}
		}
		return nil, errcode.New(errcode.ConnConsentTimeout, sessionID)
	}
	if !canTransition(sess.State, StateClosed) && !accepted {
		// 已关闭的会话再次拒绝：按已结束处理
		return nil, errcode.New(errcode.SessionNotFound, "already closed")
	}
	if !accepted {
		if !canTransition(sess.State, StateClosed) {
			return nil, errcode.New(errcode.SessionNotFound, "cannot close")
		}
		sess.State = StateClosed
		sess.Reason = reason
		sess.EndedAt = now
		if err := s.store.UpdateSession(ctx, sess); err != nil {
			return nil, errcode.New(errcode.SrvInternal, err.Error())
		}
		return nil, errcode.New(errcode.ConnConsentRejected, sessionID)
	}
	if !canTransition(sess.State, StateAuthorized) {
		return nil, errcode.New(errcode.SessionNotFound, "not requestable: "+sess.State)
	}
	sess.State = StateAuthorized
	sess.AuthExpireAt = now.Add(AuthWindow)
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return sess, nil
}

// Activate 标记会话进入 active。必须已授权且授权窗口未过期。
func (s *Service) Activate(ctx context.Context, sessionID string) (*store.Session, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errcode.New(errcode.SessionNotFound, sessionID)
	} else if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	// 阻断：未经 consent 的会话不得直接激活
	if sess.State != StateAuthorized {
		return nil, errcode.New(errcode.SessionNotFound, "not authorized: "+sess.State)
	}
	// 阻断：授权窗口缺失或已过期
	now := s.now()
	if sess.AuthExpireAt.IsZero() {
		return nil, errcode.New(errcode.SessionAuthWindowExceeded, "no auth window")
	}
	if now.After(sess.AuthExpireAt) {
		sess.State = StateClosed
		sess.Reason = "auth_window_exceeded"
		sess.EndedAt = now
		if err := s.store.UpdateSession(ctx, sess); err != nil {
			return nil, errcode.New(errcode.SrvInternal, err.Error())
		}
		return nil, errcode.New(errcode.SessionAuthWindowExceeded, sessionID)
	}
	if !canTransition(sess.State, StateActive) {
		return nil, errcode.New(errcode.SessionNotFound, "bad transition")
	}
	sess.State = StateActive
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return sess, nil
}

// Kill 关闭会话（后台强制中断或任一端主动结束）。
func (s *Service) Kill(ctx context.Context, sessionID, reason string) (*store.Session, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errcode.New(errcode.SessionNotFound, sessionID)
	} else if err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	if !canTransition(sess.State, StateClosed) {
		return nil, errcode.New(errcode.SessionNotFound, "already closed")
	}
	sess.State = StateClosed
	sess.Reason = reason
	sess.EndedAt = s.now()
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	return sess, nil
}

// Close 由任一端主动结束。
func (s *Service) Close(ctx context.Context, sessionID, reason string) (*store.Session, error) {
	return s.Kill(ctx, sessionID, reason)
}

// Get 查询会话。
func (s *Service) Get(ctx context.Context, sessionID string) (*store.Session, error) {
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
