#!/usr/bin/env bash
# 反向隧道保活：frp 抖动导致 ssh -R 断开时自动重连。
#
# 为什么需要它：Windows 客户端的 127.0.0.1:18090 完全靠这条隧道撑着，
# 断了用户只会看到"远程计算机拒绝网络连接"，而且无从排查。
set -u
SSH_ARGS=(-N -o BatchMode=yes -o ExitOnForwardFailure=yes
          -o ServerAliveInterval=15 -o ServerAliveCountMax=3
          -R 18090:127.0.0.1:18090 -R 19001:127.0.0.1:19001 win)
while true; do
  echo "[tunnel] connecting $(date "+%F %T")"
  ssh "${SSH_ARGS[@]}"
  echo "[tunnel] disconnected rc=$? $(date "+%F %T"), retry in 3s"
  sleep 3
done
