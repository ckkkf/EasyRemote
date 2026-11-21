package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

// TestFilesRoundTrip 覆盖 P0-1 的主路径：上传 → 列表 → 下载，字节必须完全一致。
func TestFilesRoundTrip(t *testing.T) {
	f := newFixture(t)
	content := []byte("EasyRemote 文件传输 smoke: 中文 + emoji 🚀 + binary \x00\x01\x02")
	status, raw := f.upload(f.acct, "smoke.txt", content)
	if status != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", status, raw)
	}
	var created struct {
		File struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"file"`
	}
	if err := unmarshalData(raw, &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	want := sha256.Sum256(content)
	if created.File.SHA256 != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256 mismatch: %s", created.File.SHA256)
	}
	if created.File.Size != int64(len(content)) {
		t.Fatalf("size = %d want %d", created.File.Size, len(content))
	}

	// 列表：只列当前账号文件，分页字段存在。
	status, raw = f.do(http.MethodGet, "/v1/files",
		nil, map[string]string{"Authorization": "Bearer " + f.acct.Token})
	if status != 200 {
		t.Fatalf("list status = %d body=%s", status, raw)
	}
	var list struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := unmarshalData(raw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Files) != 1 || list.Files[0].ID != created.File.ID || list.Files[0].Name != "smoke.txt" {
		t.Fatalf("unexpected list: %s", raw)
	}

	// 下载：需要 Bearer + 设备凭证。
	status, got := f.do(http.MethodGet, "/v1/files/"+created.File.ID, nil, map[string]string{
		"Authorization":  "Bearer " + f.acct.Token,
		"X-Device-Token": f.acct.DeviceToken,
	})
	if status != 200 {
		t.Fatalf("download status = %d body=%s", status, got)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("download bytes differ: %q", got)
	}
}

// TestFilesRequireToken 覆盖「无令牌 401」。
func TestFilesRequireToken(t *testing.T) {
	f := newFixture(t)
	status, raw := f.do(http.MethodGet, "/v1/files", nil, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", status, raw)
	}
	if name := errName(raw); name != "AUTH_TOKEN_EXPIRED" {
		t.Fatalf("want AUTH_TOKEN_EXPIRED, got %s", name)
	}

	// 有账号令牌但没有设备凭证：上传必须被拒（403），不能只凭账号令牌写文件。
	status, raw = f.do(http.MethodPost, "/v1/files", strings.NewReader(""), map[string]string{
		"Authorization": "Bearer " + f.acct.Token,
	})
	if status != http.StatusForbidden {
		t.Fatalf("upload without device token: want 403, got %d body=%s", status, raw)
	}
}

// TestFilesTooLarge 覆盖「超限 413」：把上限调到 1KiB，再传 4KiB。
func TestFilesTooLarge(t *testing.T) {
	f := newFixture(t)
	if err := f.mem.PutConfig(t.Context(), "file.max_bytes", "1024"); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	content := bytes.Repeat([]byte("x"), 4096)
	status, raw := f.upload(f.acct, "big.bin", content)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d body=%s", status, raw)
	}
	if name := errName(raw); name != "FILE_TOO_LARGE" {
		t.Fatalf("want FILE_TOO_LARGE, got %s", name)
	}
	// 超限文件不得留在列表里。
	var list struct {
		Files []map[string]any `json:"files"`
	}
	_, raw = f.do(http.MethodGet, "/v1/files", nil, map[string]string{"Authorization": "Bearer " + f.acct.Token})
	_ = unmarshalData(raw, &list)
	if len(list.Files) != 0 {
		t.Fatalf("rejected upload leaked into list: %s", raw)
	}
}

// TestFilesCrossAccountDenied 覆盖「跨账号下载 403」。
func TestFilesCrossAccountDenied(t *testing.T) {
	f := newFixture(t)
	status, raw := f.upload(f.acct, "secret.txt", []byte("owner only"))
	if status != http.StatusCreated {
		t.Fatalf("upload: %d %s", status, raw)
	}
	var created struct {
		File struct {
			ID string `json:"id"`
		} `json:"file"`
	}
	_ = unmarshalData(raw, &created)

	other := f.loginAs("13900002222")
	status, raw = f.do(http.MethodGet, "/v1/files/"+created.File.ID, nil, map[string]string{
		"Authorization":  "Bearer " + other.Token,
		"X-Device-Token": other.DeviceToken,
	})
	if status != http.StatusForbidden {
		t.Fatalf("want 403 for other account, got %d body=%s", status, raw)
	}
	// 另一个账号也不能删别人的文件。
	status, _ = f.do(http.MethodDelete, "/v1/files/"+created.File.ID, nil, map[string]string{
		"Authorization": "Bearer " + other.Token,
	})
	if status != http.StatusForbidden {
		t.Fatalf("cross-account delete: want 403, got %d", status)
	}
}

// TestFilesDeleteIsIdempotent 删除两次都返回 200。
func TestFilesDeleteIsIdempotent(t *testing.T) {
	f := newFixture(t)
	status, raw := f.upload(f.acct, "tmp.bin", []byte("bye"))
	if status != http.StatusCreated {
		t.Fatalf("upload: %d %s", status, raw)
	}
	var created struct {
		File struct {
			ID string `json:"id"`
		} `json:"file"`
	}
	_ = unmarshalData(raw, &created)
	hdrs := map[string]string{"Authorization": "Bearer " + f.acct.Token}
	for i := 0; i < 2; i++ {
		status, raw := f.do(http.MethodDelete, "/v1/files/"+created.File.ID, nil, hdrs)
		if status != 200 {
			t.Fatalf("delete #%d: want 200, got %d body=%s", i+1, status, raw)
		}
	}
}

// TestFilesDownloadTicketSingleUse 校验浏览器路径：票据只能换一次字节。
func TestFilesDownloadTicketSingleUse(t *testing.T) {
	f := newFixture(t)
	content := []byte("ticket payload")
	status, raw := f.upload(f.acct, "ticket.txt", content)
	if status != http.StatusCreated {
		t.Fatalf("upload: %d %s", status, raw)
	}
	var created struct {
		File struct {
			ID string `json:"id"`
		} `json:"file"`
	}
	_ = unmarshalData(raw, &created)

	authHeaders := map[string]string{
		"Authorization":  "Bearer " + f.acct.Token,
		"X-Device-Token": f.acct.DeviceToken,
	}
	status, raw = f.do(http.MethodPost, "/v1/files/"+created.File.ID+"/ticket", nil, authHeaders)
	if status != 200 {
		t.Fatalf("ticket: %d %s", status, raw)
	}
	var tk struct {
		URL string `json:"url"`
	}
	_ = unmarshalData(raw, &tk)
	if !hasPrefix(tk.URL, "/v1/files/") {
		t.Fatalf("unexpected ticket url: %q", tk.URL)
	}

	// 票据 URL 本身带鉴权：浏览器打开链接时没有任何请求头，必须能下载。
	status, got := f.do(http.MethodGet, tk.URL, nil, nil)
	if status != 200 || !bytes.Equal(got, content) {
		t.Fatalf("ticket download without auth: status=%d body=%q", status, got)
	}
	// 单次生效：第二次连票据都不带，必须被拒。
	status, _ = f.do(http.MethodGet, tk.URL, nil, nil)
	if status != http.StatusForbidden {
		t.Fatalf("ticket must be single use, got %d", status)
	}
	// 票据不能挪用到别的文件 id 上。
	status, raw = f.do(http.MethodPost, "/v1/files/"+created.File.ID+"/ticket", nil, authHeaders)
	if status != 200 {
		t.Fatalf("ticket #2: %d %s", status, raw)
	}
	_ = unmarshalData(raw, &tk)
	status, _ = f.do(http.MethodGet, strings.Replace(tk.URL, created.File.ID, "fil-other", 1), nil, nil)
	if status != http.StatusForbidden {
		t.Fatalf("ticket must be bound to its file, got %d", status)
	}
}
