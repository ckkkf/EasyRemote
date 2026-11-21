@echo off
rem ===========================================================================
rem step4-demo - cross-machine DEMO (Windows controller + macOS target).
rem
rem What the operator sees:
rem   - Windows sends a circle of mouse moves -> the macOS cursor physically
rem     spins on the target screen (real CGEventPost injection).
rem   - Then macOS captures its own screen and streams frames back; Windows
rem     records the H.264 bitstream to demo_mac.h264 for ffmpeg playback.
rem
rem Args:  %1 = controller phone, %2 = demo seconds (default 2)
rem Usage: ssh win "C:\EasyRemote\run_win2mac_demo.cmd [phone] [secs]"
rem Log:   C:\EasyRemote\demo2logs\win_demo.log
rem ===========================================================================
setlocal
set "CTRL_PHONE=%~1"
if "%CTRL_PHONE%"=="" set "CTRL_PHONE=13700008102"
set "SECS=%~2"
if "%SECS%"=="" set "SECS=2"
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\demo2logs"
set "LOG=%LOGDIR%\win_demo.log"
set "HELLO=%ROOT%\mac_hello.txt"
set "EXE=%ROOT%\core\target\debug\examples\win_controller.exe"
set "REC=%ROOT%\demo_mac.h264"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" 2>nul
del /q "%REC%" 2>nul

if not exist "%EXE%" (echo DEMO_RESULT: FAIL ^(win_controller.exe missing^) & exit /b 1)
if not exist "%HELLO%" (echo DEMO_RESULT: FAIL ^(mac_hello.txt missing^) & exit /b 1)

taskkill /im udptun.exe /f >nul 2>&1
start "udptun" /b "%ROOT%\udptun.exe" -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
ping -n 3 127.0.0.1 >nul

cd /d "%ROOT%\core"
"%EXE%" http://127.0.0.1:18090 127.0.0.1:19000 %CTRL_PHONE% 123456 "%HELLO%" 5 30 960 540 %SECS% "%REC%" > "%LOG%" 2>&1
set "RC=%ERRORLEVEL%"
echo DEMO_EXIT=%RC%>>"%LOG%"
type "%LOG%"
if not "%RC%"=="0" (echo DEMO_RESULT: FAIL ^(controller exit %RC%^) & exit /b 1)
findstr /c:"WIN: VERDICT PASS" "%LOG%" >nul 2>&1
if errorlevel 1 (echo DEMO_RESULT: FAIL ^(verdict^) & exit /b 1)
if not exist "%REC%" (echo DEMO_RESULT: FAIL ^(no h264 recorded^) & exit /b 1)
echo DEMO_RESULT: PASS
exit /b 0
