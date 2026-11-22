# ER-710a 证据：剪贴板 data stream
- 状态: 部分完成（opt-in 核心链路）
- 变更：er-channels MSG_CLIP/encode/decode；er-client ctrl/host 轮询与收发。
- 测试：clip_roundtrip、clip_rejects_bad_kind_and_truncation、clip_rejects_oversize PASS；gate。
- 未完成：UI/能力启用、双 OS E2E、富文本转换。
