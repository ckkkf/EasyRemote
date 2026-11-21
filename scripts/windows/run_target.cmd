@echo off
rem 跨机被控端：必须在**交互会话**里跑（Session 0 里 SendInput 会被系统静默拒绝）。
rem 注册： schtasks /create /tn ERTarget /tr "C:\EasyRemote\run_target.cmd" /sc once /st 00:00 /ru Administrator /it /f
rem 运行： schtasks /run /tn ERTarget
rem 说明：gateway/relay 走 ssh -R 映射到本机 127.0.0.1，relay 的 UDP 由 udptun client 承接。
cd /d C:\EasyRemote\core
del C:\EasyRemote\win_hello.txt 2>nul
echo TARGET_START %DATE% %TIME% > C:\EasyRemote\target.log
.\target\debug\examples\xmachine_target.exe http://127.0.0.1:18090 127.0.0.1:19000 13700005201 123456 http://127.0.0.1:18090 5 8 C:\EasyRemote\win_hello.txt 3 >> C:\EasyRemote\target.log 2>&1
echo TARGET_EXIT=%ERRORLEVEL% >> C:\EasyRemote\target.log
