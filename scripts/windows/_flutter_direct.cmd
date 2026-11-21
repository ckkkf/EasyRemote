@echo off
rem 绕过 flutter.bat（这台机器上它报 "拒绝访问/Unable to determine engine version"），
rem 直接用 Dart 跑 flutter_tools.snapshot。用法：flutter_direct.cmd <flutter 子命令>
set "FLUTTER_ROOT=C:\EasyRemote\flutter"
set "PUB_HOSTED_URL=https://pub.flutter-io.cn"
set "FLUTTER_STORAGE_BASE_URL=https://storage.flutter-io.cn"
set "FLUTTER_SUPPRESS_ANALYTICS=true"
set "CI=true"
"C:\EasyRemote\flutter\bin\cache\dart-sdk\bin\dart.exe" "C:\EasyRemote\flutter\bin\cache\flutter_tools.snapshot" %*
