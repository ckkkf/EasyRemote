@echo off
rem 把 MSVC 环境带进 cargo（否则 rustc 找不到 link.exe）。
rem 用法： C:\EasyRemote\mp.cmd cmd /c "cd /d C:\EasyRemote\core && cargo build --workspace"
call "C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul 2>&1
%*
