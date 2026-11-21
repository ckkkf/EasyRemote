// Package httpx 提供统一的 JSON 响应、错误信封与中间件。
// 所有服务共用，避免各自实现漂移。
package httpx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/easyremote/server/internal/errcode"
)

// maxBody 是请求体上限，防止单个请求吃满内存。
const maxBody = 1 << 20 // 1 MiB

// TraceID 生成短追踪 ID，写入响应头与日志，便于客服对账。
func TraceID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "trace-unavailable"
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Envelope 是错误响应的对外结构（见 docs/spec/error-codes.md §3）。
type Envelope struct {
	Code         int    `json:"code"`
	Name         string `json:"name"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMS int    `json:"retry_after_ms,omitempty"`
	TraceID      string `json:"trace_id"`
}

// JSON 写成功响应。
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Err 把结构化错误写成对外信封。
// 规则：message 取自码表（用户可读），detail 只写日志，绝不回给调用方。
func Err(w http.ResponseWriter, r *http.Request, err error) {
	trace := w.Header().Get("X-Trace-Id")
	if trace == "" {
		trace = TraceID()
		w.Header().Set("X-Trace-Id", trace)
	}

	var e *errcode.Error
	if errors.As(err, &e) {
		m := errcode.Describe(e.Code)
		log.Printf("trace=%s code=%d name=%s detail=%s", trace, e.Code, errcode.Name(e.Code), e.Detail)
		JSON(w, m.Status, Envelope{
			Code:      int(e.Code),
			Name:      errcode.Name(e.Code),
			Message:   m.Message,
			Retryable: m.Retryable,
			TraceID:   trace,
		})
		return
	}

	// 未结构化错误：一律按内部错误返回，细节进日志
	log.Printf("trace=%s unhandled error: %v", trace, err)
	m := errcode.Describe(errcode.SrvInternal)
	JSON(w, m.Status, Envelope{
		Code:      int(errcode.SrvInternal),
		Name:      errcode.Name(errcode.SrvInternal),
		Message:   m.Message,
		Retryable: m.Retryable,
		TraceID:   trace,
	})
}

// Decode 解析 JSON 请求体，带大小上限。
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Middleware 负责 trace id、panic 恢复与访问日志。
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace := r.Header.Get("X-Trace-Id")
		if trace == "" {
			trace = TraceID()
		}
		w.Header().Set("X-Trace-Id", trace)

		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("trace=%s panic: %v", trace, rec)
				m := errcode.Describe(errcode.SrvInternal)
				JSON(w, m.Status, Envelope{
					Code: int(errcode.SrvInternal), Name: errcode.Name(errcode.SrvInternal),
					Message: m.Message, Retryable: true, TraceID: trace,
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
