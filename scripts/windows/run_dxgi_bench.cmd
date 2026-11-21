@echo off
rem DXGI + NVENC 四路径基准：必须在**交互会话**里跑（Session 0 里 DXGI Desktop Duplication 取不到桌面）。
rem 注册： schtasks /create /tn ERDxgi /tr "C:\EasyRemote\run_dxgi_bench.cmd" /sc once /st 00:00 /ru Administrator /it /f
rem 运行： schtasks /run /tn ERDxgi
cd /d C:\EasyRemote\core
echo DXGI_BENCH_START %DATE% %TIME% > C:\EasyRemote\dxgi_bench.log
.\target\debug\examples\dxgi_nvenc_bench.exe %1 >> C:\EasyRemote\dxgi_bench.log 2>&1
echo DXGI_BENCH_EXIT=%ERRORLEVEL% >> C:\EasyRemote\dxgi_bench.log
