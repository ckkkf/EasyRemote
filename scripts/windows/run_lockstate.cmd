@echo off
rem ===========================================================================
rem step3 - lock-state control: on an UNLOCKED desktop, is_locked() must be false.
rem
rem Why: if lock detection misfires as 'always locked', capture permanently returns
rem Ok(None) -> session connects but no picture, with zero error lines in the log.
rem That is the hardest class of bug to find, so it gets its own assertion.
rem
rem Usage: ssh win "cmd /c C:\EasyRemote\run_lockstate.cmd"
rem Exit:  0=PASS 1=FAIL 3=SKIP(payload says locked/no-dxgi) 4=SKIP(no NVIDIA driver)
rem Log:   C:\EasyRemote\step3logs\lockstate.log
rem ===========================================================================
setlocal
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\step3logs"
set "NAME=lockstate"
set "LOG=%LOGDIR%\%NAME%.log"
set "FLAG=%LOGDIR%\%NAME%.done"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" "%FLAG%" 2>nul

call :preflight
if errorlevel 1 exit /b 4

set "PAY=%LOGDIR%\%NAME%.payload.cmd"
> "%PAY%" echo @echo off
>> "%PAY%" echo cd /d "%ROOT%\core"
>> "%PAY%" echo .\target\debug\examples\dxgi_step3.exe lockstate ^> "%LOG%" 2^>^&1
>> "%PAY%" echo echo DONE ^> "%FLAG%"
schtasks /create /tn ERStep3_%NAME% /tr "%PAY%" /sc once /st 00:00 /ru Administrator /it /f >nul 2>&1
schtasks /run /tn ERStep3_%NAME% >nul 2>&1
call :waitflag 90
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
  echo STEP3_RESULT: FAIL
  exit /b 1
