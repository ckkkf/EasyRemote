@echo off
rem Windows 侧 UDP 隧道客户端：把本机 19000 的 UDP 转发到 19001（ssh -R 到 Mac 的 udptun server）
taskkill /im udptun.exe /f >nul 2>&1
start "udptun" /b C:\EasyRemote\udptun.exe -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
ping -n 3 127.0.0.1 >nul
