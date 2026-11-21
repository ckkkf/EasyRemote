package httpx

import (
	"net/http"

	"github.com/easyremote/server/internal/errcode"
)

// OKBody 是成功响应的统一信封，与错误信封同构。
// 客户端只需要一个解析器：code==0 读 data，否则读 message。
type OKBody[T any] struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	Data    T      `json:"data"`
	TraceID string `json:"trace_id"`
}

// Page 是列表响应的统一结构。禁止再发明 {users:[...]} / {devices:[...]} 这种形状。
type Page[T any] struct {
	Items      []T    `json:"items"`
	Total      int    `json:"total"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// OK 写成功响应。
func OK[T any](w http.ResponseWriter, r *http.Request, v T) {
	JSON(w, http.StatusOK, OKBody[T]{Code: 0, Name: "OK", Data: v, TraceID: TraceOf(w, r)})
}

// OKStatus 写指定状态码的成功响应（例如 201 Created）。
func OKStatus[T any](w http.ResponseWriter, r *http.Request, status int, v T) {
	JSON(w, status, OKBody[T]{Code: 0, Name: "OK", Data: v, TraceID: TraceOf(w, r)})
}

// OKPage 写分页列表响应。
func OKPage[T any](w http.ResponseWriter, r *http.Request, p Page[T]) { OK(w, r, p) }

// TraceOf 取（或补）trace id：成功与错误响应必须带同一个 ID。
func TraceOf(w http.ResponseWriter, r *http.Request) string {
	if t := w.Header().Get("X-Trace-Id"); t != "" {
		return t
	}
	t := ""
	if r != nil {
		t = r.Header.Get("X-Trace-Id")
	}
	if t == "" {
		t = TraceID()
	}
	w.Header().Set("X-Trace-Id", t)
	return t
}

// JSONMux 给 mux 兜底：未匹配的路由也返回 JSON 信封，不再吐 text/plain。
// 用法：httpx.Middleware(httpx.JSONMux(mux))
func JSONMux(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern == "" {
			Err(w, r, errcode.New(errcode.SrvRouteNotFound, r.Method+" "+r.URL.Path))
			return
		}
		h.ServeHTTP(w, r)
	})
}
