@echo off
rem ===========================================================================
rem step3 - NVENC copy vs zero-copy comparison (interactive session only).
rem
rem dxgi_nvenc_bench.exe runs all paths in one go; this wrapper extracts the
rem METRIC lines for 'dxgi+NVENC(copy)' and 'dxgi+NVENC(zero-copy)' into a table.
rem
rem Usage: ssh win "cmd /c C:\EasyRemote\run_bench_compare.cmd"
rem Exit:  0=metrics produced 1=FAIL 3=SKIP 4=SKIP(preflight)
rem Log:   C:\EasyRemote\step3logs\bench_compare.log
rem ===========================================================================
setlocal
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\step3logs"
set "NAME=bench_compare"
set "LOG=%LOGDIR%\%NAME%.log"
set "FLAG=%LOGDIR%\%NAME%.done"
set "ITERS=%~1"
if "%ITERS%"=="" set "ITERS=20"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" "%FLAG%" 2>nul

call :preflight
if errorlevel 1 exit /b 4

set "PAY=%LOGDIR%\%NAME%.payload.cmd"
> "%PAY%" echo @echo off
>> "%PAY%" echo cd /d "%ROOT%\core"
>> "%PAY%" echo .\target\debug\examples\dxgi_nvenc_bench.exe %ITERS% ^> "%LOG%" 2^>^&1
>> "%PAY%" echo echo BENCH_EXIT=%%ERRORLEVEL%% ^>^> "%LOG%"
>> "%PAY%" echo echo DONE ^> "%FLAG%"
schtasks /create /tn ERStep3_%NAME% /tr "%PAY%" /sc once /st 00:00 /ru Administrator /it /f >nul 2>&1
schtasks /run /tn ERStep3_%NAME% >nul 2>&1
call :waitflag 300

echo ---------------- %NAME%.log (tail) ----------------
powershell -NoProfile -Command "Get-Content '%LOG%' -Tail 12"
echo ---------------------------------------------------
powershell -NoProfile -ExecutionPolicy Bypass -File "%ROOT%\bench_compare.ps1" "%LOG%"
if errorlevel 1 (echo STEP3_RESULT: FAIL & exit /b 1)
find /i "CAPTURE_BUDGET_40MS" "%LOG%" >nul 2>&1 && (echo STEP3_RESULT: PASS & exit /b 0)
echo STEP3_RESULT: FAIL ^(bench did not reach CAPTURE_BUDGET_40MS^)
exit /b 1

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
