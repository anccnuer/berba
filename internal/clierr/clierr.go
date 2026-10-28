// Package clierr 定义带稳定退出码的 CLI 错误模型。
//
// 退出码约定：1=通用错误/用法，2=配置，3=工具链，4=构建，5=校验。
package clierr

import (
	"errors"
)

type Code int

const (
	CodeUsage     Code = 1
	CodeConfig    Code = 2
	CodeToolchain Code = 3
	CodeBuild     Code = 4
	CodeVerify    Code = 5
)

type Error struct {
	Code Code
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e.Op == "" {
		return e.Err.Error()
	}
	return e.Op + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

func New(code Code, op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Op: op, Err: err}
}

// Is 报告错误链中是否已含类型化错误，避免重复包裹。
func Is(err error) bool {
	var e *Error
	return errors.As(err, &e)
}

// ExitCode 沿错误链查找最近的类型化错误，未找到时返回 1。
func ExitCode(err error) int {
	var e *Error
	if errors.As(err, &e) && e.Code != 0 {
		return int(e.Code)
	}
	return int(CodeUsage)
}
