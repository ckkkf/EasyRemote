package gateway

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/session"
	"github.com/easyremote/server/internal/store"
)

// 协助码（临时授权）设计：
//
//	被控端生成：POST /v1/assist/create，必须持「此设备自己的」X-Device-Token。
//	    这就把「谁允许谁进来」钉在设备上——光有账号令牌不能替别的设备发码。
//	主控兑换：POST /v1/assist/redeem，Bearer + 主控设备凭证，用码换 requested 会话。
//	    协助码本身就是被控端给出的授权意图，因此跳过验证码（captcha）环节。
//	一次性：兑换由存储层单条 UPDATE / 单把锁原子完成，并发双花只会有一个成功。
//	审计：成功与失败都写 assist.redeem，含来源 IP 与失败原因。
const (
	assistCodeTTL = 10 * time.Minute
	assistDigits  = 6
)

// newAssistCode 生成不与现存码冲突的 6 位数字码。
func (a *App) newAssistCode(ctx context.Context) (string, error) {
	for i := 0; i < 20; i++ {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return "", errcode.New(errcode.SrvInternal, "assist code random: "+err.Error())
		}
		code := fmt.Sprintf("%0*d", assistDigits, binary.BigEndian.Uint64(buf[:])%1_000_000)
		if _, err := a.Store.GetAssistCode(ctx, code); errors.Is(err, store.ErrNotFound) {
			return code, nil
		} else if err != nil {
			return "", errcode.New(errcode.SrvInternal, err.Error())
		}
	}
	return "", errcode.New(errcode.SrvInternal, "assist code allocation exhausted")
}

func (a *App) auditAssist(ctx context.Context, r *http.Request, actor, object, result string) {
	_ = a.Store.AppendAudit(ctx, &store.AuditLog{
		Actor:  actor,
		Role:   "client",
		Action: "assist.redeem",
		Object: object,
		IP:     clientIP(r),
		Result: result,
		TS:     time.Now().UTC(),
	})
}

func (a *App) createAssist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
		return
	}
	acc, err := a.account(ctx, r)
	if err != nil {
		WriteErr(w, err)
		return
	}
	if a.Limiter != nil {
		if res, _ := a.Limiter.Allow(ctx, "assist:create:"+acc.ID, 10, time.Minute); !res.Allowed {
			WriteErr(w, errcode.New(errcode.ConnRateLimited, "assist create rate limited"))
			return
		}
	}
	target, err := a.deviceByCode(ctx, req.DeviceCode)
	if err != nil {
		WriteErr(w, errcode.New(errcode.ConnDeviceNotFound, "device not found"))
		return
	}
	if target.AccountID != acc.ID {
		// 不能替不属于自己的设备发码。
		WriteErr(w, errcode.New(errcode.AdminForbidden, "device not owned by account"))
		return
	}
	if err := a.requireDevice(ctx, r, target.ID); err != nil {
		WriteErr(w, err)
		return
	}
	code, err := a.newAssistCode(ctx)
	if err != nil {
		WriteErr(w, err)
		return
	}
	exp := time.Now().UTC().Add(assistCodeTTL)
	if err := a.Store.PutAssistCode(ctx, &store.AssistCode{
		Code:      code,
		AccountID: acc.ID,
		TargetID:  target.ID,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: exp,
	}); err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	httpx.OK(w, r, contract.AssistCreateResult{
		Code:           code,
		ExpiresAt:      exp,
		TargetDeviceID: target.ID,
		TtlSeconds:     int(assistCodeTTL.Seconds()),
	})
}

func (a *App) redeemAssist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(w, r, &req); err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, "bad body"))
		return
	}
	acc, err := a.account(ctx, r)
	if err != nil {
		WriteErr(w, err)
		return
	}
	if a.Limiter != nil {
		if res, _ := a.Limiter.Allow(ctx, "assist:redeem:"+acc.ID, 20, time.Minute); !res.Allowed {
			WriteErr(w, errcode.New(errcode.ConnRateLimited, "assist redeem rate limited"))
			return
		}
	}
	controllerID, err := a.deviceFromCredential(ctx, r, acc.ID)
	if err != nil {
		WriteErr(w, err)
		return
	}
	code := strings.TrimSpace(req.Code)
	if len(code) != assistDigits {
		a.auditAssist(ctx, r, acc.ID, code, "denied:malformed")
		WriteErr(w, errcode.New(errcode.ConnAssistCodeInvalid, "code must be 6 digits"))
		return
	}

	rec, err := a.Store.GetAssistCode(ctx, code)
	if errors.Is(err, store.ErrNotFound) {
		a.auditAssist(ctx, r, acc.ID, code, "denied:not_found")
		WriteErr(w, errcode.New(errcode.ConnAssistCodeInvalid, "code not found"))
		return
	} else if err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	now := time.Now().UTC()
	if rec.Redeemed() {
		a.auditAssist(ctx, r, acc.ID, code, "denied:used")
		WriteErr(w, errcode.New(errcode.ConnAssistCodeUsed, "code already redeemed"))
		return
	}
	if !now.Before(rec.ExpiresAt) {
		a.auditAssist(ctx, r, acc.ID, code, "denied:expired")
		WriteErr(w, errcode.New(errcode.ConnAssistCodeExpired, "code expired"))
		return
	}
	if rec.TargetID == controllerID {
		a.auditAssist(ctx, r, acc.ID, code, "denied:self")
		WriteErr(w, errcode.New(errcode.ConnSelfConnect, "self connect"))
		return
	}
	ok, err := a.Store.RedeemAssistCode(ctx, code, now)
	if err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	if !ok {
		// 并发下被别人先兑换了：重新读一次，给出准确原因。
		if latest, getErr := a.Store.GetAssistCode(ctx, code); getErr == nil && latest.Redeemed() {
			a.auditAssist(ctx, r, acc.ID, code, "denied:used")
			WriteErr(w, errcode.New(errcode.ConnAssistCodeUsed, "code already redeemed"))
			return
		}
		a.auditAssist(ctx, r, acc.ID, code, "denied:expired")
		WriteErr(w, errcode.New(errcode.ConnAssistCodeExpired, "code expired"))
		return
	}
	sess, err := a.Session.Request(ctx, controllerID, rec.TargetID)
	if err != nil {
		a.auditAssist(ctx, r, acc.ID, code, "error:"+errcode.Name(errCodeOf(err)))
		WriteErr(w, err)
		return
	}
	if a.Notifier != nil && sess.State == session.StateRequested {
		_ = a.Notifier.NotifyConsentRequired(sess.ID, controllerID, rec.TargetID)
	}
	a.auditAssist(ctx, r, acc.ID, code, "ok")
	httpx.OK(w, r, contract.AssistRedeemResult{
		SessionID: sess.ID,
		State:     sess.State,
		TargetID:  rec.TargetID,
	})
}

// errCodeOf 取结构化错误码用于审计，非结构化错误记为 SRV_INTERNAL。
func errCodeOf(err error) errcode.Code {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return errcode.SrvInternal
}
