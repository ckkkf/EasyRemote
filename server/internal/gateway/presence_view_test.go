package gateway

import (
	"net/http"
	"testing"
)

// 设备在线视图必须与信令 presence 一致，而不是绑定表里的历史布尔值。
func TestDeviceOnlineReadsPresence(t *testing.T) {
	f := newFixture(t)
	acct := f.loginAs("13800005001")

	var out struct {
		Devices []struct {
			Online bool `json:"online"`
		} `json:"devices"`
	}

	f.app.Presence = fakePresence{online: false}
	if status := f.getJSON("/v1/devices", authHdr(acct), &out); status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if len(out.Devices) != 1 || out.Devices[0].Online {
		t.Fatalf("presence=false must show offline: %+v", out.Devices)
	}

	f.app.Presence = fakePresence{online: true}
	if status := f.getJSON("/v1/devices", authHdr(acct), &out); status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if len(out.Devices) != 1 || !out.Devices[0].Online {
		t.Fatalf("presence=true must show online: %+v", out.Devices)
	}
}
