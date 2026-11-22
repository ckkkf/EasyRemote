#!/usr/bin/env bash
# ER-711d：真实进程内文件传输（主控→被控）。启用 ER_FILE_SEND_PATH/ER_FILE_RECV_DIR。
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT="docs/impl/evidence/ER-711d"
mkdir -p "$OUT/recv"
SRC="$OUT/send.bin"
python3 - "$SRC" <<'PY'
import os,sys
path=sys.argv[1]
with open(path,'wb') as f:
    f.write(bytes((i % 251) for i in range(200000)))
PY
rm -f "$OUT/recv/send.bin"
cd client
ER_FILE_SEND_PATH="$(cd ..; pwd)/$OUT/send.bin" \
ER_FILE_RECV_DIR="$(cd ..; pwd)/$OUT/recv" \
  flutter test tool/file_transfer_e2e_test.dart
