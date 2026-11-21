@echo off
rem ===========================================================================
rem step3 - cross-machine session (Mac controller + Windows target), GPU zero-copy.
rem
rem Two phases on purpose:
rem   Windows (this script): preflight -> start target in the interactive session
rem            via schtasks /IT -> write device_code/captcha -> wait for the Mac
rem            side to write C:\EasyRemote\gpu_session.done -> assert the log
rem            contains backend=gpu-zero-copy / backend_final=gpu-zero-copy and
rem            print frames received, resolution, cursor position after injection.
rem   Mac (scripts/mac/step3_xmachine.sh): start stack/relay/udptun, open ssh -R
rem            tunnels, read hello, run the controller, scp the done flag back.
rem
rem The controller is a Mac binary and the Windows box has no key for the Mac,
rem so a single 'one-click' on Windows would have to fake the other half.
rem
rem Args:  %1 = target phone (default 13700007101), %2 = controller phone (default 13700007102)
rem Usage: ssh win "cmd /c C:\EasyRemote\run_xmachine_gpu.cmd [target_phone] [ctrl_phone]"
rem Exit:  0=PASS 1=FAIL 3=SKIP 4=SKIP(preflight)
rem Log:   C:\EasyRemote\step3logs\xmachine_gpu.log
rem ===========================================================================
setlocal
set "WIN_PHONE=%~1"
if "%WIN_PHONE%"=="" set "WIN_PHONE=13700007101"
set "MAC_PHONE=%~2"
if "%MAC_PHONE%"=="" set "MAC_PHONE=13700007102"
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\step3logs"
set "NAME=xmachine_gpu"
set "LOG=%LOGDIR%\%NAME%.log"
set "HELLO=%ROOT%\win_hello.txt"
set "DONE=%ROOT%\gpu_session.done"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" "%HELLO%" "%DONE%" 2>nul

call :preflight
if errorlevel 1 exit /b 4
rem Authoritative lock check: OpenInputDesktop only works inside the interactive
rem session, so launch dxgi_step3 lockstate via schtasks instead of probing here.
call :lockcheck
if errorlevel 1 (echo STEP3_RESULT: SKIP ^(desktop locked, unlock first^) & exit /b 4)

set "PAY=%LOGDIR%\%NAME%.payload.cmd"
> "%PAY%" echo @echo off
>> "%PAY%" echo cd /d "%ROOT%\core"
>> "%PAY%" echo .\target\debug\examples\xmachine_target.exe http://127.0.0.1:18090 127.0.0.1:19000 %WIN_PHONE% 123456 http://127.0.0.1:18090 5 8 "%HELLO%" 3 ^> "%LOG%" 2^>^&1
>> "%PAY%" echo echo TARGET_EXIT=%%ERRORLEVEL%% ^>^> "%LOG%"
schtasks /create /tn ERStep3_%NAME% /tr "%PAY%" /sc once /st 00:00 /ru Administrator /it /f >nul 2>&1
schtasks /run /tn ERStep3_%NAME% >nul 2>&1

echo Waiting for the target to publish device_code/captcha ...
call :waitfile "%HELLO%" 60
if errorlevel 1 (echo STEP3_RESULT: FAIL ^(target did not produce hello^) & exit /b 1)
echo ---------------- hello ----------------
type "%HELLO%"
echo --------------------------------------
echo.
echo Now on the Mac run:
echo     bash scripts/mac/step3_xmachine.sh %MAC_PHONE%
echo.
echo Waiting for the Mac side to finish ...
call :waitfile "%DONE%" 300
if errorlevel 1 (echo STEP3_RESULT: FAIL ^(mac side never reported done^) & exit /b 1)

echo ---------------- %NAME%.log ----------------
type "%LOG%" 2>nul
echo -------------------------------------------
call :verdict
exit /b %ERRORLEVEL%

:lockcheck
  set "LC=%LOGDIR%\lockcheck.log"
  set "LCFLAG=%LOGDIR%\lockcheck.done"
  del /q "%LC%" "%LCFLAG%" 2>nul
  set "LCPAY=%LOGDIR%\lockcheck.payload.cmd"
  > "%LCPAY%" echo @echo off
  >> "%LCPAY%" echo cd /d "%ROOT%\core"
  >> "%LCPAY%" echo .\target\debug\examples\dxgi_step3.exe lockstate ^> "%LC%" 2^>^&1
  >> "%LCPAY%" echo echo DONE ^> "%LCFLAG%"
  schtasks /create /tn ERStep3_lockchk /tr "%LCPAY%" /sc once /st 00:00 /ru Administrator /it /f >nul 2>&1
  schtasks /run /tn ERStep3_lockchk >nul 2>&1
  call :waitfile "%LCFLAG%" 60
  find /i "is_locked=false" "%LC%" >nul 2>&1
  if errorlevel 1 exit /b 1
  exit /b 0

:preflight
  rem NOTE: do NOT gate on LogonUI.exe presence.
  rem LogonUI stays resident even when the desktop is unlocked, so that check
  rem produced false SKIPs. The authoritative test is OpenInputDesktop, which
  rem only works from the interactive session - so the payload decides.
  if not exist "%SystemRoot%\System32\nvEncodeAPI64.dll" (
     echo SKIP: nvEncodeAPI64.dll not found ^(no NVIDIA driver^).
     exit /b 1
  )
  exit /b 0

:waitfile
  set /a _n=0
:waitloop
  if exist %1 exit /b 0
  set /a _n+=1
  if %_n% GEQ %2 (
     echo TIMEOUT waiting for %1
     exit /b 1
  )
  ping -n 2 127.0.0.1 >nul
  goto waitloop

:verdict
  find /i "CODEC: backend=gpu-zero-copy" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3_RESULT: FAIL ^(backend=gpu-zero-copy not found^) & exit /b 1)
  find /i "CODEC: backend_final=gpu-zero-copy" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3_RESULT: FAIL ^(backend_final is not gpu-zero-copy^) & exit /b 1)
  find /i "moved_to_target=true" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3_RESULT: FAIL ^(cursor did not move to target^) & exit /b 1)
  find /i "frames_received=" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3_RESULT: FAIL ^(no frames_received line^) & exit /b 1)
  echo STEP3_RESULT: PASS
  exit /b 0
