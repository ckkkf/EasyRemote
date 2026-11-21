# ffnvcodec / NVENC 头文件

- `nvEncodeAPI.h`：来自 FFmpeg/nv-codec-headers master，版本 **NVENC API 13.1**
  （`NVENCAPI_MAJOR_VERSION 13` / `MINOR 1`），2026-11-20 取回。
- 用途：只作为 **bindgen 的输入**，用它在开发机上生成 `er-codec/src/nvenc/bindings.rs`；
  运行时**不链接**任何 CUDA/NVENC 库，全部走 `LoadLibraryA` 动态解析。

## 重新生成 bindings

```bash
cargo install bindgen-cli --root /tmp/bindgen --locked
/tmp/bindgen/bin/bindgen core/crates/er-codec/third_party/ffnvcodec/nvEncodeAPI.h \
    --no-layout-tests --no-doc-comments --formatter prettyplease \
    -o core/crates/er-codec/src/nvenc/bindings.rs

# 头文件里的 GUID 是 static const，bindgen 会生成 extern static（链接期找不到符号），
# 生成后必须删掉这些行（GUID 数值在 src/nvenc/mod.rs 里手写成常量）：
python3 - <<'EOF'
import re
p = 'core/crates/er-codec/src/nvenc/bindings.rs'
lines = [l for l in open(p).read().split('\n') if not re.match(r'\s*pub static [A-Za-z0-9_]+: GUID;', l)]
open(p, 'w').write('\n'.join(lines))
EOF
```

## 头文件里的版本宏（bindgen 不生成，mod.rs 里手写）

`NVENCAPI_STRUCT_VERSION(ver) = NVENCAPI_VERSION | (ver << 16) | (0x7 << 28)`，
其中 `NVENCAPI_VERSION = 0x0100000D`（13 | 1<<24）。
