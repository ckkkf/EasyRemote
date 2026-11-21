@echo off
cd /d C:\EasyRemote\core
echo PROBE_START %DATE% %TIME% > C:\EasyRemote\dxgi_probe.log
.\target\debug\examples\dxgi_probe.exe %1 >> C:\EasyRemote\dxgi_probe.log 2>&1
echo PROBE_EXIT=%ERRORLEVEL% >> C:\EasyRemote\dxgi_probe.log
