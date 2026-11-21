@echo off
rem EasyRemote 启动器：先起本地 UDP 隧道，再起主控端 GUI
cd /d "%~dp0"
taskkill /im udptun.exe /f >nul 2>&1
if exist udptun.exe start "" /b udptun.exe -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
ping -n 3 127.0.0.1 >nul
start "" EasyRemote.exe
