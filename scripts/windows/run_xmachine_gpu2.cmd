@echo off
rem ===========================================================================
rem step3b - cross-machine session, CLEAN evidence (zero-copy + link + input).
rem
rem What changed vs run_xmachine_gpu.cmd:
rem   1) screen motion is produced by a window that moves itself (scene_move.ps1).
rem      It never touches the cursor, so the injection-precision measurement is no
rem      longer contaminated by the perturbation (round 37 used jiggle.ps1).
rem   2) chcp 65001 first, so the Rust (UTF-8) log lines are readable and the file
rem      is valid UTF-8 end to end.
rem   3) the target asserts perturbation_stopped=true (two identical cursor
rem      baselines AFTER the scene is killed, BEFORE injection).
rem
rem Usage: ssh win "cmd /c C:\EasyRemote\run_xmachine_gpu2.cmd [target_phone] [ctrl_phone] [scene_secs]"
rem Exit:  0=PASS 1=FAIL 3=SKIP 4=SKIP(preflight)
rem Log:   C:\EasyRemote\step3logs\xmachine2.log
rem ===========================================================================
setlocal
chcp 65001 >nul
set "WIN_PHONE=%~1"
if "%WIN_PHONE%"=="" set "WIN_PHONE=13700008101"
set "MAC_PHONE=%~2"
if "%MAC_PHONE%"=="" set "MAC_PHONE=13700008102"
set "SCENE=%~3"
if "%SCENE%"=="" set "SCENE=14"
set "ROOT=C:\EasyRemote"
set "LOGDIR=%ROOT%\step3logs"
set "NAME=xmachine2"
set "LOG=%LOGDIR%\%NAME%.log"
set "HELLO=%ROOT%\win_hello2.txt"
set "DONE=%ROOT%\gpu_session2.done"
if not exist "%LOGDIR%" mkdir "%LOGDIR%"
del /q "%LOG%" "%HELLO%" "%DONE%" 2>nul

rem clear leftover scene windows from a previous round
taskkill /im powershell.exe /f >nul 2>&1

set "PAY=%LOGDIR%\%NAME%.payload.cmd"
> "%PAY%" echo @echo off
>> "%PAY%" echo chcp 65001 ^>nul
>> "%PAY%" echo cd /d "%ROOT%"
>> "%PAY%" echo taskkill /im udptun.exe /f ^>nul 2^>^&1
>> "%PAY%" echo start "udptun" /b "%ROOT%\udptun.exe" -mode client -listen 127.0.0.1:19000 -server 127.0.0.1:19001
>> "%PAY%" echo ping -n 3 127.0.0.1 ^>nul
>> "%PAY%" echo cd /d "%ROOT%\core"
>> "%PAY%" echo target\debug\examples\xmachine_target.exe http://127.0.0.1:18090 127.0.0.1:19000 %WIN_PHONE% 123456 http://127.0.0.1:18090 5 10 "%HELLO%" 3 %SCENE% ^> "%LOG%" 2^>^&1
>> "%PAY%" echo echo TARGET_EXIT=%%ERRORLEVEL%% ^>^> "%LOG%"
>> "%PAY%" echo echo DONE ^> "%DONE%"
schtasks /create /tn ERStep3b_%NAME% /tr "%PAY%" /sc once /st 00:00 /ru Administrator /it /f >nul 2>&1
schtasks /run /tn ERStep3b_%NAME% >nul 2>&1

echo Waiting for the target to publish device_code/captcha ...
call :waitfile "%HELLO%" 90
if errorlevel 1 (echo STEP3B_RESULT: FAIL ^(target did not produce hello^) & exit /b 1)
echo ---------------- hello ----------------
type "%HELLO%"
echo --------------------------------------
echo.
echo Now on the Mac run:  bash scripts/mac/step3_xmachine2.sh %MAC_PHONE%
echo.
echo Waiting for the Mac side to finish ...
call :waitfile "%DONE%" 420
if errorlevel 1 (echo STEP3B_RESULT: FAIL ^(mac side never reported done^) & exit /b 1)

echo ---------------- %NAME%.log ----------------
type "%LOG%" 2>nul
echo -------------------------------------------
call :verdict
exit /b %ERRORLEVEL%

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
  if errorlevel 1 (echo STEP3B_RESULT: FAIL ^(backend=gpu-zero-copy not found^) & exit /b 1)
  find /i "CODEC: backend_final=gpu-zero-copy" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3B_RESULT: FAIL ^(backend_final is not gpu-zero-copy^) & exit /b 1)
  find /i "moved_to_target=true" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3B_RESULT: FAIL ^(input did not land within +-2px^) & exit /b 1)
  find /i "perturbation_stopped=true" "%LOG%" >nul 2>&1
  if errorlevel 1 (echo STEP3B_RESULT: FAIL ^(cursor baseline unstable: perturbation not proven stopped^) & exit /b 1)
  findstr /c:"frames_sent=0 " "%LOG%" >nul 2>&1
  if not errorlevel 1 (echo STEP3B_RESULT: FAIL ^(frames_sent=0^) & exit /b 1)
  findstr /c:"frames_received=0 " "%LOG%" >nul 2>&1
  if not errorlevel 1 (echo STEP3B_RESULT: FAIL ^(frames_received=0^) & exit /b 1)
  echo STEP3B_RESULT: PASS
  exit /b 0
