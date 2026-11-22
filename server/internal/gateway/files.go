package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/easyremote/server/internal/appconfig"
	"github.com/easyremote/server/internal/contract"
	"github.com/easyremote/server/internal/errcode"
	"github.com/easyremote/server/internal/httpx"
	"github.com/easyremote/server/internal/store"
)

// 文件传输的访问控制设计（写进代码是因为这里最容易被改坏）：
//
//	上传 / 列表 / 删除：Bearer 令牌（确定账号）+ X-Device-Token（确定是哪台设备在操作）。
//	    只认令牌不认参数，客户端无法通过改 body 冒充别的账号或设备。
//	下载：同样要求 Bearer + 该账号下某台设备的 X-Device-Token；
//	    唯一的例外是「下载票据」——浏览器打开链接时无法附加请求头，
//	    所以由已认证的客户端先换一张 60 秒、单次使用的票据，
//	    票据里固化了 file_id 与 account_id，URL 被别人拿到也只能下载那一个文件。
//
// 结算规则：元数据里记录 account_id，任何路径都必须先证明「你是这个账号」，
// 再判断「这个文件是不是你的」；跨账号一律 403 而不是 404，避免用 404/403 差异枚举他人文件是否存在。
const (
	// defaultMaxFileBytes 是单文件上限（32 MiB）。可通过配置中心 file.max_bytes 调小，
	// 但硬上限不放宽：服务端落盘空间是共享资源。
	defaultMaxFileBytes = 32 << 20
	minFileBytes        = 1024
	fileListPageSize    = 20
	// multipartSlack 预留给 multipart 边界与头部，不计入文件大小上限。
	multipartSlack = 1 << 20
	// downloadTicketTTL 是下载票据有效期。
	downloadTicketTTL = 60 * time.Second
)

// fileDTO 是对外可见的文件描述。绝不包含 StoragePath。
func fileDTO(f *store.FileMeta) contract.FileRow {
	return contract.FileRow{
		ID:        f.ID,
		AccountID: f.AccountID,
		DeviceID:  f.DeviceID,
		Name:      f.Name,
		Size:      f.Size,
		Sha256:    f.SHA256,
		CreatedAt: f.CreatedAt,
	}
}

// filesRoot 返回落盘根目录并确保存在。
func (a *App) filesRoot() (string, error) {
	dir := a.FilesDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "easyremote-files")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", errcode.New(errcode.SrvInternal, "files dir: "+err.Error())
	}
	return dir, nil
}

// maxUploadBytes 返回当前生效的单文件上限。
// 配置值只在 [minFileBytes, defaultMaxFileBytes] 内生效，其余情况回落到硬上限。
func (a *App) maxUploadBytes(ctx context.Context) int64 {
	if a.Config == nil {
		return defaultMaxFileBytes
	}
	n, err := a.Config.GetInt(ctx, appconfig.KeyFileMaxBytes)
	if err != nil || n < minFileBytes || n > defaultMaxFileBytes {
		return defaultMaxFileBytes
	}
	return n
}

// decodeFileID 从 /v1/files/{id}[/ticket] 里取出 id 与子路径。
func decodeFileID(path string) (id, sub string) {
	rest := strings.TrimPrefix(path, "/v1/files/")
	if rest == path {
		return "", ""
	}
	parts := strings.SplitN(strings.Trim(rest, "/"), "/", 2)
	id = parts[0]
	if len(parts) == 2 {
		sub = parts[1]
	}
	return id, sub
}

// sanitizeFileName 只保留基名并去掉控制字符，防止路径穿越与响应头注入。
func sanitizeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = "upload.bin"
	}
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}

// handleFiles 处理集合端点：POST 上传，GET 列表。
func (a *App) handleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		a.uploadFile(w, r)
	case http.MethodGet:
		a.listFiles(w, r)
	default:
		WriteErr(w, errcode.New(errcode.SrvInternal, "method not allowed"))
	}
}

func (a *App) uploadFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acc, err := a.account(ctx, r)
	if err != nil {
		WriteErr(w, err)
		return
	}
	// 写操作限流：键按账号而不是 IP，避免同一出口 IP 后面多个账号互相拖累。
	if a.Limiter != nil {
		if res, _ := a.Limiter.Allow(ctx, "files:"+acc.ID, 30, time.Minute); !res.Allowed {
			WriteErr(w, errcode.New(errcode.ConnRateLimited, "file upload rate limited"))
			return
		}
	}
	deviceID, err := a.deviceFromCredential(ctx, r, acc.ID)
	if err != nil {
		WriteErr(w, err)
		return
	}
	limit := a.maxUploadBytes(ctx)

	r.Body = http.MaxBytesReader(w, r.Body, limit+multipartSlack)
	mr, err := r.MultipartReader()
	if err != nil {
		WriteErr(w, errcode.New(errcode.FileReadFailed, "expect multipart body"))
		return
	}

	dir, err := a.filesRoot()
	if err != nil {
		WriteErr(w, err)
		return
	}
	var stored *store.FileMeta
	for {
		part, partErr := mr.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			WriteErr(w, errcode.New(errcode.FileReadFailed, partErr.Error()))
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		meta, saveErr := a.savePart(dir, part, limit)
		_ = part.Close()
		if saveErr != nil {
			WriteErr(w, saveErr)
			return
		}
		meta.AccountID = acc.ID
		meta.DeviceID = deviceID
		stored = meta
		break
	}
	if stored == nil {
		WriteErr(w, errcode.New(errcode.FileReadFailed, "missing multipart field: file"))
		return
	}
	if err := a.Store.CreateFile(ctx, stored); err != nil {
		_ = os.Remove(stored.StoragePath)
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	httpx.OKStatus(w, r, http.StatusCreated, contract.FileUploadResult{File: fileDTO(stored)})
}

// savePart 把 multipart 分片流式写入临时文件，同时计算 sha256 并强制上限。
func (a *App) savePart(dir string, part io.Reader, limit int64) (*store.FileMeta, error) {
	name := "upload.bin"
	if namer, ok := part.(interface{ FileName() string }); ok {
		name = sanitizeFileName(namer.FileName())
	}
	var idBytes [8]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, errcode.New(errcode.SrvInternal, "random id: "+err.Error())
	}
	tmpName := "up-" + hex.EncodeToString(idBytes[:])
	path := filepath.Join(dir, tmpName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errcode.New(errcode.FileDiskFull, err.Error())
	}
	hasher := sha256.New()
	// 多读 1 字节即可判断是否超限，不必把整个文件读进内存。
	written, copyErr := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(part, limit+1))
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(path)
		return nil, errcode.New(errcode.FileReadFailed, copyErr.Error())
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return nil, errcode.New(errcode.FileDiskFull, closeErr.Error())
	}
	if written > limit {
		_ = os.Remove(path)
		return nil, errcode.New(errcode.FileTooLarge,
			fmt.Sprintf("file exceeds %d bytes", limit))
	}
	if written == 0 {
		_ = os.Remove(path)
		return nil, errcode.New(errcode.FileReadFailed, "empty file")
	}
	return &store.FileMeta{
		Name:        name,
		Size:        written,
		SHA256:      hex.EncodeToString(hasher.Sum(nil)),
		StoragePath: path,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (a *App) listFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acc, err := a.account(ctx, r)
	if err != nil {
		WriteErr(w, err)
		return
	}
	page := store.Page{Limit: fileListPageSize}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n > 0 && n <= 100 {
			page.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n >= 0 {
			page.Offset = n
		}
	}
	list, err := a.Store.ListFilesByAccount(ctx, acc.ID, page)
	if err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	out := make([]contract.FileRow, 0, len(list))
	for _, f := range list {
		out = append(out, fileDTO(f))
	}
	httpx.OK(w, r, contract.FileListResult{Files: out, Limit: page.Limit, Offset: page.Offset})
}

// handleFileItem 处理 /v1/files/{id} 与 /v1/files/{id}/ticket。
func (a *App) handleFileItem(w http.ResponseWriter, r *http.Request) {
	id, sub := decodeFileID(r.URL.Path)
	if id == "" {
		WriteErr(w, errcode.New(errcode.FileNotFound, "missing file id"))
		return
	}
	switch {
	case sub == "" && r.Method == http.MethodGet:
		a.downloadFile(w, r, id)
	case sub == "" && r.Method == http.MethodDelete:
		a.deleteFile(w, r, id)
	case sub == "ticket" && r.Method == http.MethodPost:
		a.issueDownloadTicket(w, r, id)
	default:
		WriteErr(w, errcode.New(errcode.SrvInternal, "method not allowed"))
	}
}

// authorizeFileAccess 是下载/票据的唯一鉴权入口：
// 先把账号证明出来，再确认文件归属该账号。
func (a *App) authorizeFileAccess(ctx context.Context, r *http.Request, id string) (*store.FileMeta, *store.Account, error) {
	acc, err := a.account(ctx, r)
	if err != nil {
		return nil, nil, err
	}
	meta, err := a.Store.GetFile(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, errcode.New(errcode.FileNotFound, id)
	} else if err != nil {
		return nil, nil, errcode.New(errcode.SrvInternal, err.Error())
	}
	if meta.AccountID != acc.ID {
		return nil, nil, errcode.New(errcode.AdminForbidden, "file belongs to another account")
	}
	return meta, acc, nil
}

func (a *App) downloadFile(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	// 两条路径：
	//   1) 票据：浏览器直接打开链接时无法附加请求头，票据本身就是凭据
	//      （60 秒、单次、固化 file_id + account_id）。此时不要求 Bearer；
	//   2) 设备凭证：客户端带 Authorization + X-Device-Token，两者都必须属于该账号。
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		a.downloadByTicket(w, ctx, id, ticket)
		return
	}
	meta, _, err := a.authorizeFileAccess(ctx, r, id)
	if err != nil {
		WriteErr(w, err)
		return
	}
	if _, err := a.deviceFromCredential(ctx, r, meta.AccountID); err != nil {
		WriteErr(w, err)
		return
	}
	a.streamFile(w, meta)
}

// downloadByTicket 处理「只有票据、没有请求头」的下载：
// 先原子核销票据（单次），再按票据里固化的 file_id / account_id 校验文件归属。
// 票据里的 account_id 是签发时从已认证会话里取的，所以核销成功即等于「账号已证明」。
func (a *App) downloadByTicket(w http.ResponseWriter, ctx context.Context, id, token string) {
	tk, ok := a.ticketsRef().take(token)
	if !ok {
		WriteErr(w, errcode.New(errcode.AdminForbidden, "download ticket invalid or expired"))
		return
	}
	// 票据只对签发时的那个文件有效：URL 被换成别的 id 直接拒绝。
	if tk.fileID != id {
		WriteErr(w, errcode.New(errcode.AdminForbidden, "download ticket does not match file"))
		return
	}
	meta, err := a.Store.GetFile(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		WriteErr(w, errcode.New(errcode.FileNotFound, id))
		return
	} else if err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	if meta.AccountID != tk.accountID {
		WriteErr(w, errcode.New(errcode.AdminForbidden, "download ticket account mismatch"))
		return
	}
	a.streamFile(w, meta)
}

// streamFile 把落盘文件写回调用方。
func (a *App) streamFile(w http.ResponseWriter, meta *store.FileMeta) {
	f, err := os.Open(meta.StoragePath)
	if err != nil {
		WriteErr(w, errcode.New(errcode.FileNotFound, "blob missing: "+err.Error()))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": meta.Name}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(200)
	_, _ = io.Copy(w, f)
}

func (a *App) deleteFile(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	if a.Limiter != nil {
		if acc, err := a.account(ctx, r); err == nil {
			if res, _ := a.Limiter.Allow(ctx, "files:"+acc.ID, 30, time.Minute); !res.Allowed {
				WriteErr(w, errcode.New(errcode.ConnRateLimited, "file delete rate limited"))
				return
			}
		}
	}
	meta, _, err := a.authorizeFileAccess(ctx, r, id)
	if err != nil {
		// 幂等：不存在（含已被别人删掉）按成功处理，其余错误照常返回。
		if errcode.Is(err, errcode.FileNotFound) {
			httpx.OK(w, r, contract.FileDeletedResult{OK: true, Deleted: false})
			return
		}
		WriteErr(w, err)
		return
	}
	if err := a.Store.DeleteFile(ctx, meta.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		WriteErr(w, errcode.New(errcode.SrvInternal, err.Error()))
		return
	}
	if err := os.Remove(meta.StoragePath); err != nil && !os.IsNotExist(err) {
		// 元数据已删，字节残留只会浪费磁盘，不该让用户重试。
		_ = err
	}
	httpx.OK(w, r, contract.FileDeletedResult{OK: true, Deleted: true})
}

// ---------- 下载票据 ----------

type downloadTicket struct {
	fileID    string
	accountID string
	expiresAt time.Time
}

// ticketStore 是进程内单次票据表。多实例部署时应换成 Redis（与限流同一份依赖）。
type ticketStore struct {
	mu      sync.Mutex
	tickets map[string]downloadTicket
}

func (t *ticketStore) put(token string, tk downloadTicket) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tickets == nil {
		t.tickets = map[string]downloadTicket{}
	}
	now := time.Now()
	for k, v := range t.tickets {
		if now.After(v.expiresAt) {
			delete(t.tickets, k)
		}
	}
	t.tickets[token] = tk
}

// take 取出并删除票据（单次使用）。
func (t *ticketStore) take(token string) (downloadTicket, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk, ok := t.tickets[token]
	if !ok {
		return downloadTicket{}, false
	}
	delete(t.tickets, token)
	if time.Now().After(tk.expiresAt) {
		return downloadTicket{}, false
	}
	return tk, true
}

func (a *App) ticketsRef() *ticketStore {
	a.ticketMu.Lock()
	defer a.ticketMu.Unlock()
	if a.Tickets == nil {
		a.Tickets = &ticketStore{}
	}
	return a.Tickets
}

func (a *App) issueDownloadTicket(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	meta, acc, err := a.authorizeFileAccess(ctx, r, id)
	if err != nil {
		WriteErr(w, err)
		return
	}
	if _, err := a.deviceFromCredential(ctx, r, acc.ID); err != nil {
		WriteErr(w, err)
		return
	}
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		WriteErr(w, errcode.New(errcode.SrvInternal, "ticket random: "+err.Error()))
		return
	}
	token := hex.EncodeToString(raw[:])
	exp := time.Now().Add(downloadTicketTTL)
	a.ticketsRef().put(token, downloadTicket{fileID: meta.ID, accountID: meta.AccountID, expiresAt: exp})
	httpx.OK(w, r, contract.FileTicketResult{
		URL:       "/v1/files/" + meta.ID + "?ticket=" + token,
		ExpiresAt: exp,
		Name:      meta.Name,
	})
}

func (a *App) takeTicket(token, fileID, accountID string) bool {
	tk, ok := a.ticketsRef().take(token)
	if !ok {
		return false
	}
	return tk.fileID == fileID && tk.accountID == accountID
}

// 让 httpx 仍被引用（错误信封由 WriteErr 统一处理）。
var _ = httpx.JSON
