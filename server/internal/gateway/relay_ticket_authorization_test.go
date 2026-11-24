package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

type ticketEpochFunc func(context.Context, string, string) (int64, error)

func (f ticketEpochFunc) NextTicketEpoch(ctx context.Context, sessionID, deviceID string) (int64, error) {
	return f(ctx, sessionID, deviceID)
}

func TestRelayTicketGateRejectsInvalidStateAndExpiredAuthorization(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800007110")
	outsider := f.loginAs("13800007111")
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	f.app.Session.SetClock(func() time.Time { return now })
	f.app.Relay.SetClock(func() time.Time { return now })
	var rotations atomic.Int32
	f.app.TicketEpochs = ticketEpochFunc(func(context.Context, string, string) (int64, error) {
		rotations.Add(1)
		return 1, nil
	})
	for _, tc := range []struct {
		name, state string
		deadline    time.Time
		caller      *accountFixture
		want        errcode.Code
	}{
		{"requested", session.StateRequested, now.Add(time.Hour), f.acct, errcode.SessionAuthWindowExceeded},
		{"closed", session.StateClosed, now.Add(time.Hour), target, errcode.SessionAuthWindowExceeded},
		{"missing expiry", session.StateAuthorized, time.Time{}, f.acct, errcode.SessionAuthWindowExceeded},
		{"authorized at deadline", session.StateAuthorized, now, f.acct, errcode.SessionAuthWindowExceeded},
		{"active past deadline", session.StateActive, now.Add(-time.Second), target, errcode.SessionAuthWindowExceeded},
		{"outsider after deadline", session.StateAuthorized, now.Add(-time.Second), outsider, errcode.AdminForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := &store.Session{ControllerID: f.acct.DeviceID, TargetID: target.DeviceID, State: tc.state, StartedAt: now, AuthExpireAt: tc.deadline}
			if err := f.mem.CreateSession(t.Context(), sess); err != nil {
				t.Fatal(err)
			}
			status, body := f.postJSONRaw("/v1/relay/ticket", map[string]string{"session_id": sess.ID}, authHdr(tc.caller))
			assertTicketDenial(t, status, body, tc.want)
			if rotations.Load() != 0 {
				t.Error("ticket rotation happened for a rejected request")
			}
		})
	}
}

func assertTicketDenial(t *testing.T, status int, body []byte, want errcode.Code) {
	t.Helper()
	var envelope struct {
		Code    errcode.Code    `json:"code"`
		Name    string          `json:"name"`
		TraceID string          `json:"trace_id"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusForbidden || envelope.Code != want || envelope.Name != errcode.Name(want) ||
		envelope.TraceID == "" || len(envelope.Data) != 0 {
		t.Errorf("unexpected denial: status=%d code=%d name=%q trace present=%t data present=%t",
			status, envelope.Code, envelope.Name, envelope.TraceID != "", len(envelope.Data) != 0)
	}
}

func TestRelayTicketExpiresWithinAuthorizationWindow(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800007112")
	now := time.Date(2026, 11, 24, 0, 0, 0, 250_000_000, time.UTC)
	f.app.Session.SetClock(func() time.Time { return now })
	f.app.Relay.SetClock(func() time.Time { return now })
	for _, tc := range []struct {
		state          string
		window, maxTTL time.Duration
		caller, peer   *accountFixture
	}{
		{session.StateAuthorized, 3 * time.Hour, time.Hour, f.acct, target},
		{session.StateActive, 2 * time.Minute, 2 * time.Minute, target, f.acct},
	} {
		t.Run(tc.state, func(t *testing.T) {
			sess := &store.Session{ControllerID: f.acct.DeviceID, TargetID: target.DeviceID, State: tc.state, StartedAt: now, AuthExpireAt: now.Add(tc.window)}
			if err := f.mem.CreateSession(t.Context(), sess); err != nil {
				t.Fatal(err)
			}
			var result contract.RelayTicketResult
			if status := f.postJSON("/v1/relay/ticket", map[string]string{"session_id": sess.ID}, authHdr(tc.caller), &result); status != http.StatusOK {
				t.Fatalf("valid authorization did not issue ticket: %d", status)
			}
			ticket, err := f.app.Relay.Verify(result.Ticket)
			if err != nil {
				t.Fatal(err)
			}
			want := now.Add(tc.maxTTL).Truncate(time.Second)
			if result.SelfDevice != tc.caller.DeviceID || ticket.SessionID != sess.ID || ticket.Self != tc.caller.DeviceID ||
				ticket.Peer != tc.peer.DeviceID || !ticket.ExpiresAt.Equal(want) {
				t.Error("ticket bound to wrong devices, session or deadline")
			}
			at := now
			f.app.Relay.SetClock(func() time.Time { return want })
			defer f.app.Relay.SetClock(func() time.Time { return at })
			if _, err := f.app.Relay.Verify(result.Ticket); !errcode.Is(err, errcode.ConnRelayUnavailable) {
				t.Errorf("ticket accepted at its expiry: %v", err)
			}
		})
	}
}

func TestRelayTicketDeniesExpiryDuringEpochRotation(t *testing.T) {
	f := newFixture(t)
	target := f.loginAs("13800007113")
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	deadline := now.Add(time.Minute)
	var rotated atomic.Bool
	clock := func() time.Time {
		if rotated.Load() {
			return deadline
		}
		return now
	}
	f.app.Session.SetClock(clock)
	f.app.Relay.SetClock(clock)
	f.app.TicketEpochs = ticketEpochFunc(func(context.Context, string, string) (int64, error) {
		rotated.Store(true)
		return 1, nil
	})
	sess := &store.Session{ControllerID: f.acct.DeviceID, TargetID: target.DeviceID, State: session.StateAuthorized, StartedAt: now, AuthExpireAt: deadline}
	if err := f.mem.CreateSession(t.Context(), sess); err != nil {
		t.Fatal(err)
	}
	status, body := f.postJSONRaw("/v1/relay/ticket", map[string]string{"session_id": sess.ID}, authHdr(f.acct))
	if !rotated.Load() {
		t.Fatal("test did not pass the first check before simulating a slow rotation")
	}
	assertTicketDenial(t, status, body, errcode.SessionAuthWindowExceeded)
}
