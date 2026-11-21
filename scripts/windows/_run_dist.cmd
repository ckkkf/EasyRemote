@echo off
cd /d C:\EasyRemote\dist\EasyRemote-Windows
taskkill /im udptun.exe /f >nul 2>&1
taskkill /im EasyRemote.exe /f >nul 2>&1
start "" /b udptun.exe -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
ping -n 3 127.0.0.1 >nul
start "" EasyRemote.exe
