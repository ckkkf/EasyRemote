@echo off
cd /d C:\EasyRemote\core
taskkill /im er-winapp.exe /f >nul 2>&1
start "" /b C:\EasyRemote\core\target\debug\er-winapp.exe
