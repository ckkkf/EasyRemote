@echo off
rem ===========================================================================
rem step4 - cross-machine session (Windows controller + macOS target).
rem
rem Mirror of step3: the Mac is the TARGET this time, Windows is the CONTROLLER.
rem The controller only sends input and receives frames, so it does NOT need the
rem interactive desktop and can run inside an SSH session (no schtasks /IT).
rem The Mac side (scripts/mac/step4_win2mac.sh) starts stack/relay/udptun and the
rem macOS target in the Aqua session, then scp the hello file here and invoke us.
rem
rem Args:  %1 = controller phone (default 13700008102)
rem Usage: ssh win "cmd /c C:\EasyRemote\run_win2mac.cmd [ctrl_phone]"
rem Exit:  0=PASS 1=FAIL
rem Log:   C:\EasyRemote\step4logs\win_controller.log
rem ===========================================================================
setlocal
set "CTRL_PHONE=%~1"
if "%CTRL_PHONE%"=="" set "CTRL_PHONE=13700008102"
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\step4logs"
set "LOG=%LOGDIR%\win_controller.log"
set "HELLO=%ROOT%\mac_hello.txt"
set "EXE=%ROOT%\core\target\debug\examples\win_controller.exe"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" 2>nul

if not exist "%EXE%" (
  echo STEP4_RESULT: FAIL ^(win_controller.exe not built^)
  exit /b 1
)
if not exist "%HELLO%" (
  echo STEP4_RESULT: FAIL ^(mac_hello.txt missing^)
  exit /b 1
)

rem Local UDP relay tunnel: 19000 -> 19001 (ssh -R back to the Mac).
taskkill /im udptun.exe /f >nul 2>&1
start "udptun" /b "%ROOT%\udptun.exe" -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
ping -n 3 127.0.0.1 >nul

echo ---------------- hello ----------------
type "%HELLO%"
echo --------------------------------------
echo.

cd /d "%ROOT%\core"
call :run
exit /b %ERRORLEVEL%

:run
"%EXE%" http://127.0.0.1:18090 127.0.0.1:19000 %CTRL_PHONE% 123456 "%HELLO%" 5 5 400 300 > "%LOG%" 2>&1
set "RC=%ERRORLEVEL%"
echo CONTROL_EXIT=%RC%>>"%LOG%"
type "%LOG%"
if not "%RC%"=="0" exit /b 1
findstr /c:"WIN: VERDICT PASS" "%LOG%" >nul 2>&1
if errorlevel 1 (echo STEP4_RESULT: FAIL ^(controller verdict not PASS^) & exit /b 1)
findstr /c:"frames_from_target=" "%LOG%" >nul 2>&1
if errorlevel 1 (echo STEP4_RESULT: FAIL ^(no frames from mac^) & exit /b 1)
echo STEP4_RESULT: PASS
exit /b 0
