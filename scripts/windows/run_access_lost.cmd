@echo off
rem ===========================================================================
rem step3 - ACCESS_LOST self-heal verification (interactive session only).
rem
rem What it does (dxgi_step3.exe selfheal):
rem   1) open DXGI capture + NVENC(DIRECTX) session on the SAME d3d11 device, encode baseline
rem   2) switch display mode (same resolution / different refresh first, else 1280x720)
rem   3) next AcquireNextFrame returns DXGI_ERROR_ACCESS_LOST -> rebuild_duplication()
rem   4) restore mode, encode again with the SAME encoder instance
rem Assertions: rebuilds_after > rebuilds_before
rem             device_ptr_unchanged=true      (d3d11 device NOT recreated)
rem             nvenc_session_reused=true      (NVENC session NOT recreated)
rem
rem Usage: ssh win "cmd /c C:\EasyRemote\run_access_lost.cmd"
rem Exit:  0=PASS 1=FAIL/INCONCLUSIVE 3=SKIP 4=SKIP(preflight)
rem Log:   C:\EasyRemote\step3logs\access_lost.log
rem ===========================================================================
setlocal
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\step3logs"
set "NAME=access_lost"
set "LOG=%LOGDIR%\%NAME%.log"
set "FLAG=%LOGDIR%\%NAME%.done"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" "%FLAG%" 2>nul

call :preflight
if errorlevel 1 exit /b 4

set "PAY=%LOGDIR%\%NAME%.payload.cmd"
> "%PAY%" echo @echo off
>> "%PAY%" echo cd /d "%ROOT%\core"
>> "%PAY%" echo .\target\debug\examples\dxgi_step3.exe selfheal ^> "%LOG%" 2^>^&1
>> "%PAY%" echo echo DONE ^> "%FLAG%"
schtasks /create /tn ERStep3_%NAME% /tr "%PAY%" /sc once /st 00:00 /ru Administrator /it /f >nul 2>&1
schtasks /run /tn ERStep3_%NAME% >nul 2>&1
call :waitflag 240
echo ---------------- %NAME%.log ----------------
type "%LOG%" 2>nul
echo -------------------------------------------
call :verdict
exit /b %ERRORLEVEL%

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

:waitflag
  set /a _n=0
:waitloop
  if exist "%FLAG%" exit /b 0
  set /a _n+=1
  if %_n% GEQ %1 (
     echo TIMEOUT: payload did not finish in %1s
     exit /b 1
  )
  ping -n 2 127.0.0.1 >nul
  goto waitloop

:verdict
  find /i "RESULT: PASS" "%LOG%" >nul 2>&1 && (echo STEP3_RESULT: PASS & exit /b 0)
  find /i "RESULT: SKIP" "%LOG%" >nul 2>&1 && (echo STEP3_RESULT: SKIP & exit /b 3)
  find /i "RESULT: INCONCLUSIVE" "%LOG%" >nul 2>&1 && (echo STEP3_RESULT: INCONCLUSIVE & exit /b 1)
  echo STEP3_RESULT: FAIL
  exit /b 1
