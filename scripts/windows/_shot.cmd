@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File C:\EasyRemote\shot.ps1 > C:\EasyRemote\shot.log 2>&1
echo DONE >> C:\EasyRemote\shot.log
