package capability

import (
	"os"
	"strings"
)

// Specs 返回台账快照（按 YAML 顺序）；调用方不得修改内部数据。
func Specs() []Spec {
	out := make([]Spec, len(generatedSpecs))
	copy(out, generatedSpecs)
	return out
}

// Lookup 按 key 查台账；未登记返回 false。
func Lookup(key string) (Spec, bool) {
	for _, s := range generatedSpecs {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

// committed 表示该成熟度是否达到「可对外承诺」：只有 beta/stable。
//
// experimental 只能在开发构建里被使用；disabled 任何构建都不可用。
// 服务端不提供把 experimental/disabled 变成 enabled 的环境变量——
// 否则一次误配就能把未闭环能力重新暴露出去。
func committed(m Maturity) bool {
	return m == MaturityBeta || m == MaturityStable
}

// disabledByEnv 读 ER_CAPABILITY_DISABLE（逗号分隔）：只做减法，用于线上紧急收口。
func disabledByEnv(key string) bool {
	raw := strings.TrimSpace(os.Getenv("ER_CAPABILITY_DISABLE"))
	if raw == "" {
		return false
	}
	for _, item := range strings.Split(raw, ",") {
		if strings.TrimSpace(item) == key {
			return true
		}
	}
	return false
}

// Enabled 返回该能力在本服务端构建里是否可对外承诺。
//
// ER_CAPABILITY_ENABLE 故意不被读取：服务端只能关，不能把未成熟能力打开。
func Enabled(key string) bool {
	spec, ok := Lookup(key)
	if !ok {
		return false
	}
	return committed(spec.Maturity) && !disabledByEnv(key)
}

// Public 返回给客户端/后台只读的能力开关表（key → 是否可承诺）。
func Public() map[string]bool {
	out := make(map[string]bool, len(generatedSpecs))
	for _, s := range generatedSpecs {
		out[s.Key] = committed(s.Maturity) && !disabledByEnv(s.Key)
	}
	return out
}
