@echo off
cd /d C:\EasyRemote\core
start "" /b target\debug\winit_probe.exe > C:\EasyRemote\probe.log 2>&1
ping -n 1 -w 500 127.0.0.1 >nul
