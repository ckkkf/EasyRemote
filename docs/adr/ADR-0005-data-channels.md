# ADR-0005: 文件/剪贴板暂不暴露（ER-710/711）

- 状态: 接受（v1 隐藏文件入口；剪贴板保持 disabled）
- 日期: 2026-11-22
- 关联: A-14、ER-710/711

## 背景

Flutter 文件页当前走网关 HTTP 文件存储（上传/下载到服务端），不是会话 data stream；用户会误以为文件直接传给对端。剪贴板只有状态机模块，没有系统剪贴板接入与会话传输。

## 决策

1. 将 file_transfer 能力降为 disabled，隐藏文件入口（发布与开发都不显示）。
2. clipboard_sync 维持 disabled。
3. 保留 er-channels 的文件分块/校验与剪贴板状态机作为后续实现基础，但不接到生产 UI。
4. 只有真正通过会话 data stream（P2P/Relay）完成双向传输、流控、断线续传与 hash 校验后，才升级能力。

## 后果

- 用户暂时不能使用文件/剪贴板功能；不会把网关存储冒充点对点。
- 需要后续卡实现数据通道与 OS 剪贴板适配。

## 验证

- capability 测试：debug/release 均不可用 file_transfer。
- shell 不渲染文件入口。
