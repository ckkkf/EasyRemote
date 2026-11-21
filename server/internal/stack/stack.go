// Package stack 组装共享同一存储的完整服务，供本地联调与集成测试使用。
// 生产部署仍按 cmd/gateway、cmd/admin、cmd/rendezvous、cmd/relay 分离。
package stack

import (
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/config"
)

// DevSMS 是开发用短信通道：固定码 123456。
type DevSMS struct{}

func (d *DevSMS) Send(phone, code string) error { return nil }
func (d *DevSMS) Verify(phone, code string) error {
	if code == "123456" {
		return nil
	}
	return errBadCode
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errBadCode = constErr("invalid code")

// DevTOTPSecret 是开发管理员的两步验证密钥，仅用于本地环境与集成测试。
const DevTOTPSecret = "DEVTOTPSECRET"

// devSMS 返回开发用短信通道。生产环境返回 nil，
// 迫使装配方注入真实通道（否则 BuildGateway 直接报错）。
func DevSMSProvider() auth.SMSProvider { return devSMS() }

func devSMS() auth.SMSProvider {
	if config.IsProduction() {
		return nil
	}
	return &DevSMS{}
}
