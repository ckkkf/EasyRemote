@echo off
cd /d C:\EasyRemote\client\build\windows\x64\runner\Release
taskkill /im easyremote_client.exe /f >nul 2>&1
start "" easyremote_client.exe
