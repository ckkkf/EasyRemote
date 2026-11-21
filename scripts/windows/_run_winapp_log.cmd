@echo off
rem 启动 GUI 主控端前必须先把 udptun client 拉起来：
rem app 连的是 127.0.0.1:19000（本地 UDP），没有监听者时 Windows 会回 ICMP port unreachable，
rem 下一次 recv 直接报 WSAECONNRESET(10054)——表现为「握手校验失败」。
taskkill /im udptun.exe /f >nul 2>&1
start "udptun" /b C:\EasyRemote\udptun.exe -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
ping -n 3 127.0.0.1 >nul
cd /d C:\EasyRemote\core
target\debug\er-winapp.exe > C:\EasyRemote\app.log 2>&1
echo EXIT=%ERRORLEVEL% >> C:\EasyRemote\app.log
