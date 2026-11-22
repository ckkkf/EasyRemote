package stack

import (
	"context"
	"net/http"
	"time"

	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
)

// ReadyCheck 是依赖健康探针：返回 nil 表示该依赖可用。
type ReadyCheck struct {
	Name string
	Code errcode.Code
	Fn   func(context.Context) error
}

// MountReady 挂载 /readyz：所有依赖在 2s 内可用才 200，否则 503 错误信封。
// /healthz 保持进程存活语义，不受依赖影响（ER-903e）。
func MountReady(next http.Handler, checks ...ReadyCheck) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		for _, c := range checks {
			if err := c.Fn(ctx); err != nil {
				code := c.Code
				if code == 0 {
					code = errcode.SrvInternal
				}
				httpx.ErrStatus(w, r, errcode.New(code, c.Name+": "+err.Error()), http.StatusServiceUnavailable)
				return
			}
		}
		httpx.OK(w, r, contract.OkResult{OK: true})
	})
}
