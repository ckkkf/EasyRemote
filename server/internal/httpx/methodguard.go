package httpx

import (
	"net/http"
	"sort"
	"strings"

	"github.com/easyremote/server/internal/errcode"
)

// MethodGuard 按契约声明（RouteSpec）拒绝错误 HTTP method。
//
// 为什么放在中间件而不是每个 handler 里手写：method 是路由契约的一部分，
// 统一按声明表校验才能保证「契约声明 = 运行时行为」；handler 只处理业务。
//
// 只对声明过的精确路径生效；带路径参数的子路由（如 /v1/files/{id}）由具体
// handler 自行校验（ER-013 会把它们登记进契约）。
func MethodGuard(specs []RouteSpec, next http.Handler) http.Handler {
	exact := make(map[string]map[string]bool, len(specs))
	prefix := make(map[string]map[string]bool)
	for _, s := range specs {
		target := exact
		if strings.HasSuffix(s.Path, "/") {
			// 带路径参数的子资源用集合前缀登记：/v1/files/ 覆盖 /v1/files/{id} 等。
			target = prefix
		}
		if target[s.Path] == nil {
			target[s.Path] = map[string]bool{}
		}
		target[s.Path][s.Method] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods, ok := exact[r.URL.Path]
		if !ok {
			for p, m := range prefix {
				if strings.HasPrefix(r.URL.Path, p) {
					methods, ok = m, true
					break
				}
			}
		}
		if ok && !methods[r.Method] {
			names := make([]string, 0, len(methods))
			for m := range methods {
				names = append(names, m)
			}
			sort.Strings(names)
			w.Header().Set("Allow", strings.Join(names, ", "))
			Err(w, r, errcode.New(errcode.SrvMethodNotAllowed, r.Method+" "+r.URL.Path))
			return
		}
		next.ServeHTTP(w, r)
	})
}
