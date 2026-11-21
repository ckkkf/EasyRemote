@echo off
rem NVENC 验证：必须在**交互会话**里跑（Session 0 抓不到真屏幕，会退化成合成帧）。
rem 注册： schtasks /create /tn ERBench /tr "C:\EasyRemote\run_bench.cmd" /sc once /st 00:00 /ru Administrator /it /f
rem 运行： schtasks /run /tn ERBench
cd /d C:\EasyRemote\core
echo BENCH_START %DATE% %TIME% > C:\EasyRemote\bench.log
.\target\debug\examples\nvenc_bench.exe 1920 1080 30 >> C:\EasyRemote\bench.log 2>&1
echo BENCH_EXIT=%ERRORLEVEL% >> C:\EasyRemote\bench.log
