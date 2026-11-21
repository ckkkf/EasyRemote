// Package errcode 定义全平台错误码。
// 码值由 docs/spec/error-codes.md 生成（scripts/gen-errors.mjs），本文件只放类型与工具函数。
package errcode

import "fmt"

// Code 是 6 位错误码。分支判断请用 Name()，不要用数字。
type Code int

// Entry 是错误码与常量名的配对。
type Entry struct {
	Code Code
	Name string
}

// Name 返回错误码的常量名；未注册返回 UNKNOWN。
func Name(c Code) string {
	if n, ok := generatedNames[c]; ok {
		return n
	}
	return "UNKNOWN"
}

// All 返回全部已注册错误码（按码值升序）。
func All() []Entry { return generatedAll }

// Segment 返回码段（1..9），用于校验码段划分规则。
func Segment(c Code) int { return int(c) / 100000 }

// Error 是携带错误码的结构化错误。
type Error struct {
	Code   Code
	Detail string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s: %s", e.Code, Name(e.Code), e.Detail)
}

// New 构造结构化错误。
func New(c Code, detail string) *Error { return &Error{Code: c, Detail: detail} }

// Is 判断 err 是否为指定错误码。
func Is(err error, c Code) bool {
	e, ok := err.(*Error)
	return ok && e.Code == c
}
