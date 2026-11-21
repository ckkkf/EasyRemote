@echo off
cd /d C:\EasyRemote\core
> C:\EasyRemote\dxgi_probe.log echo PROBE_START %DATE% %TIME% nonudge
.\target\debug\examples\dxgi_probe.exe nonudge >> C:\EasyRemote\dxgi_probe.log 2>&1
echo PROBE_EXIT=%ERRORLEVEL% >> C:\EasyRemote\dxgi_probe.log
