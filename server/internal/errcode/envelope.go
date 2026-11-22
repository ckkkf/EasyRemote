package errcode

import "net/http"

// Meta 是错误码对外的展示契约：用户文案、HTTP 状态、是否可重试。
// 单一来源，避免 handler 各写一套而漂移。
type Meta struct {
	Message   string
	Status    int
	Retryable bool
}

// meta 表只覆盖需要「非默认行为」的码；未列出的走 defaultMeta。
var meta = map[Code]Meta{
	AuthInvalidCredential: {Message: "手机号或验证码不正确", Status: http.StatusUnauthorized},
	AuthTokenExpired:      {Message: "登录已过期，请重新登录", Status: http.StatusUnauthorized},
	AuthTooManyAttempts:   {Message: "尝试次数过多，请稍后再试", Status: http.StatusTooManyRequests, Retryable: true},
	AuthAccountBanned:     {Message: "账号已被限制使用", Status: http.StatusForbidden},
	AuthSmsRateLimited:    {Message: "发送过于频繁，请稍后再试", Status: http.StatusTooManyRequests, Retryable: true},

	ConnDeviceNotFound:   {Message: "设备不存在或未开启被控", Status: http.StatusBadRequest},
	ConnDeviceOffline:    {Message: "对方设备不在线", Status: http.StatusBadRequest},
	ConnCaptchaInvalid:   {Message: "验证码不正确", Status: http.StatusBadRequest, Retryable: true},
	ConnCaptchaExpired:   {Message: "验证码已过期，请查看新的验证码", Status: http.StatusBadRequest},
	ConnCaptchaLocked:    {Message: "验证码错误次数过多，请稍后再试", Status: http.StatusTooManyRequests},
	ConnConsentRejected:  {Message: "对方拒绝了本次连接", Status: http.StatusForbidden},
	ConnConsentTimeout:   {Message: "对方未响应，连接已取消", Status: http.StatusRequestTimeout, Retryable: true},
	ConnDeviceBusy:       {Message: "该设备正在被其他设备控制", Status: http.StatusConflict},
	ConnRelayUnavailable: {Message: "当前网络无法建立连接，请稍后重试", Status: http.StatusServiceUnavailable, Retryable: true},
	ConnRateLimited:      {Message: "操作过于频繁，请稍后再试", Status: http.StatusTooManyRequests, Retryable: true},
	ConnSelfConnect:      {Message: "不能连接到本机", Status: http.StatusBadRequest},

	ConnAssistCodeInvalid: {Message: "协助码不正确，请核对后重试", Status: http.StatusBadRequest},
	ConnAssistCodeExpired: {Message: "协助码已过期，请在对方设备上重新生成", Status: http.StatusBadRequest},
	ConnAssistCodeUsed:    {Message: "协助码已被使用，请重新生成", Status: http.StatusBadRequest},

	FileNotFound:     {Message: "文件不存在或已删除", Status: http.StatusNotFound},
	FileTooLarge:     {Message: "文件超过大小上限", Status: http.StatusRequestEntityTooLarge},
	FileReadFailed:   {Message: "文件读取失败，请重试", Status: http.StatusInternalServerError, Retryable: true},
	FileDiskFull:     {Message: "服务端存储空间不足", Status: http.StatusInsufficientStorage},
	FileHashMismatch: {Message: "文件校验失败，请重试", Status: http.StatusBadRequest, Retryable: true},

	SessionNotFound:           {Message: "会话已结束", Status: http.StatusNotFound},
	SessionExpired:            {Message: "会话已过期", Status: http.StatusGone},
	SessionTerminatedByAdmin:  {Message: "本次会话已被中断", Status: http.StatusForbidden},
	SessionAuthWindowExceeded: {Message: "本次授权已到期，请重新连接", Status: http.StatusForbidden},

	SrvInternal:         {Message: "服务异常，请稍后重试", Status: http.StatusInternalServerError, Retryable: true},
	SrvRouteNotFound:    {Message: "请求的接口不存在", Status: http.StatusNotFound},
	SrvMethodNotAllowed: {Message: "请求方式不正确", Status: http.StatusMethodNotAllowed},
	SrvMaintenance:      {Message: "系统维护中", Status: http.StatusServiceUnavailable, Retryable: true},
	AdminForbidden:      {Message: "没有权限执行该操作", Status: http.StatusForbidden},
	AdminMfaRequired:    {Message: "请完成二次验证", Status: http.StatusUnauthorized},
	AdminSessionExpired: {Message: "登录已过期，请重新登录", Status: http.StatusUnauthorized},
	AdminReasonRequired: {Message: "请填写操作原因", Status: http.StatusBadRequest},
	AdminConfigInvalid:  {Message: "配置值不合法", Status: http.StatusBadRequest},
}

var defaultMeta = Meta{Message: "请求未完成", Status: http.StatusBadRequest}

// Describe 返回错误码的对外契约。
func Describe(c Code) Meta {
	if m, ok := meta[c]; ok {
		return m
	}
	return defaultMeta
}
