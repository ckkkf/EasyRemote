@echo off
rem Flutter 构建环境（国内镜像 + SDK PATH），用法：flutter_env.cmd <命令>
set "PATH=C:\EasyRemote\flutter\bin;%PATH%"
set "PUB_HOSTED_URL=https://pub.flutter-io.cn"
set "FLUTTER_STORAGE_BASE_URL=https://storage.flutter-io.cn"
set "FLUTTER_SUPPRESS_ANALYTICS=true"
%*
