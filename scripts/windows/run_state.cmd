@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File C:\EasyRemote\desktop_state.ps1 > C:\EasyRemote\desktop_state.log 2>&1
echo STATE_EXIT=%ERRORLEVEL% >> C:\EasyRemote\desktop_state.log
