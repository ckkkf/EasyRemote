#!/usr/bin/env python3
"""ER-906: macOS cross-process test of the production desktop FFI.

Run from the repository root after cargo build -p er-ffi. A live display and
screen capture permission are required. No frame data or credentials are saved.
"""
import ctypes as c
import json
import os
import pathlib
import socket
import subprocess as sp
import sys
import tempfile
import time
import urllib.error
import urllib.request
from contextlib import ExitStack

ROOT = pathlib.Path(__file__).resolve().parents[2]
STR = c.c_char_p

def core():
    lib = c.CDLL(str(ROOT / "core/target/debug/liber_ffi.dylib"))
    lib.er_free_string.argtypes = [c.c_void_p]
    lib.er_free_string.restype = None
    return lib

def native(lib, name, types, *args, result=c.c_void_p):
    f = getattr(lib, name)
    f.argtypes, f.restype = types, result
    value = f(*(x.encode() if isinstance(x, str) else x for x in args))
    if result != c.c_void_p:
        return value
    try:
        assert value, name
        return json.loads(c.string_at(value))
    finally:
        if value:
            lib.er_free_string(value)

def http(path, body=None, dev=None):
    h = {}
    if dev:
        h.update(Authorization="Bearer " + dev["token"])
        h["X-Device-Token"] = dev["device_token"]
    if body is not None:
        h["Content-Type"] = "application/json"
    req = urllib.request.Request(
        GW + path, None if body is None else json.dumps(body).encode(), h
    )
    try:
        with urllib.request.urlopen(req, timeout=3) as response:
            return json.load(response)
    except urllib.error.HTTPError as e:
        raise RuntimeError(f"{path}: {e.code}: {json.load(e).get('name')}") from None

def check(path, body=None, dev=None):
    result = http(path, body, dev)
    assert result.get("code") == 0, (path, result.get("name"))
    return result["data"]

def device(platform):
    a = check("/v1/auth/login", {"phone": "13700009101", "code": "123456"})
    return a | check("/v1/devices/bind", {"platform": platform}, a | {"device_token": ""})

def until(fn, seconds, label):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        value = fn()
        if value:
            return value
        time.sleep(0.05)
    raise TimeoutError(label)

def host():
    lib = core()
    d = device("macos")
    prepared = native(lib, "er_host_prepare_ex", [STR]*6, GW, RELAY,
                      d["token"], d["device_id"], d["raw_code"], d["device_token"])
    assert prepared["ok"], prepared
    h = prepared["host_id"]
    assert native(lib, "er_host_start_stream", [STR,STR,c.c_uint32,c.c_uint64,c.c_int32],
                  h, GW, 30, 90, 0)["ok"]
    try:
        pending = lambda: native(lib, "er_host_pending_consent", [STR], h)
        def wait_request():
            return until(
                lambda: (p if (p := pending()).get("ok") else None),
                15,
                "no FFI pending consent",
            )

        cancelled = wait_request()
        (pathlib.Path.cwd() / "cancel-ready").touch()
        until(
            lambda: pending().get("session_id") != cancelled["session_id"],
            3,
            "cancelled authorization remained pending",
        )
        assert not native(lib, "er_host_decide_consent", [STR, STR, c.c_int32],
                          h, cancelled["session_id"], 1)["ok"]
        print("HOST_CANCEL_CLEARED", flush=True)
        rejected = wait_request()
        assert native(lib, "er_host_decide_consent", [STR, STR, c.c_int32],
                      h, rejected["session_id"], 0)["ok"]
        print("HOST_REJECTION_SENT", flush=True)
        p = wait_request()
        print("HOST_CONSENT_RECEIVED", flush=True)
        assert native(lib, "er_host_decide_consent", [STR,STR,c.c_int32],
                      h,p["session_id"],1)["ok"]
        info = lambda: native(lib, "er_host_stream_status", [STR], h)
        until(lambda: info().get("frames"), 25, "host has not sent a real frame")
        print("HOST_FRAME_SENT", flush=True)
        until(lambda: not info()["session_id"], 15, "host retained closed session")
        print("HOST_CLOSE_PROPAGATED", flush=True)
    finally:
        native(lib, "er_host_stop_stream", [STR], h, result=c.c_int32)
        native(lib, "er_host_release", [STR], h, result=c.c_int32)

def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]

def gateway_ready():
    try:
        return http("/healthz").get("code") == 0
    except urllib.error.URLError:
        return False

def kill(proc):
    if proc.poll() is None:
        proc.terminate()
        proc.wait(timeout=5)

def main():
    global GW, RELAY
    gateway = f"127.0.0.1:{port()}"
    GW = "http://" + gateway
    RELAY = f"127.0.0.1:{port()}"
    with ExitStack() as scope:
        tmp = pathlib.Path(scope.enter_context(tempfile.TemporaryDirectory()))
        # Never inherit deployment DB, Redis, public endpoints, or secrets.
        env = {
            "PATH": os.environ["PATH"],
            "HOME": str(tmp),
            "ER_E2E_HARNESS": "1",
            "ER_PUBLIC_RELAY_ADDR": RELAY,
            "ER_RATELIMIT_OFF": "1",
        }
        for name, addr in (("stack", gateway), ("relay", RELAY)):
            binary = tmp / name
            sp.run(["go", "build", "-o", str(binary), f"./cmd/{name}"], cwd=ROOT/"server", check=True)
            log = scope.enter_context(open(tmp / f"{name}.log", "w"))
            scope.callback(kill, sp.Popen([str(binary), "-addr", addr], cwd=tmp, env=env,
                                          stdout=log, stderr=log))
        until(gateway_ready, 5, "gateway is not healthy")
        hostproc = sp.Popen([sys.executable, str(ROOT / "scripts/mac/e2e_desktop_entry.py"), GW, RELAY], cwd=tmp, env=env)
        scope.callback(kill, hostproc)
        ctl = device("probe")
        # This isolated stack has one WebSocket client (the host). /v1/devices
        # alone can report a bound device before its signalling socket exists.
        until(lambda: check("/debug/presence")["devices"] > 0, 5,
              "host has no live signaling lease")

        def target_online():
            devs = check("/v1/devices", dev=ctl)["devices"]
            return next((d["raw_code"] for d in devs if d["platform"] == "macos" and d["online"]), None)
        def request():
            target = until(target_online, 5, "host device code is missing")
            s = check("/v1/connect/request", {"device_code": target, "direct": True}, ctl)
            assert s["state"] == "requested", "local consent was not required"
            return s["session_id"]

        def status(sid):
            return check("/v1/connect/status?session_id=" + sid, dev=ctl)

        cancelled = request()
        until((tmp / "cancel-ready").exists, 5, "host did not see request before cancellation")
        check("/v1/sessions/close", {"session_id": cancelled, "reason": "controller_cancel"}, ctl)
        rejected = request()
        assert rejected != cancelled
        until(lambda: status(rejected)["state"] == "closed", 5, "host rejection not propagated")
        assert status(rejected)["reason"] == "user_rejected"
        sid = request()
        assert sid not in (cancelled, rejected)
        state = lambda: status(sid)["state"]
        until(lambda: state() == "authorized", 10, "host authorization not propagated")
        lib = core()
        attached = native(lib,"er_ctrl_attach",[STR]*6,GW,RELAY,ctl["token"],
                          ctl["device_id"],ctl["device_token"],sid)
        assert attached["ok"], attached
        handle = attached["session_id"]
        try:
            until(lambda: state() == "active", 5, "both sides failed to become active")
            def frames():
                stats = native(lib, "er_ctrl_info", [STR], handle)
                return stats if stats.get("frames") else None
            frame = until(frames, 10, "controller did not decode first frame")
            w, h = frame["width"], frame["height"]
            buf = (c.c_uint8 * (w*h*4))()
            n = native(lib, "er_ctrl_frame", [STR, c.c_void_p, c.c_size_t],
                       handle, buf, len(buf), result=c.c_int32)
            assert n == len(buf) and buf[3] == 255
            print(f"FFI_FIRST_FRAME {w}x{h} bytes={n}", flush=True)
            closed = check("/v1/sessions/close", {"session_id": sid, "reason": "controller_closed"}, ctl)
            assert closed["state"] == "closed"
            assert hostproc.wait(timeout=5) == 0
        finally:
            native(lib, "er_ctrl_close", [STR], handle, result=c.c_int32)
    print("ER906_NATIVE_CROSS_PROCESS_PASS")

if __name__ == "__main__":
    if len(sys.argv) == 3:
        GW, RELAY = sys.argv[1:]
        host()
    else:
        main()
