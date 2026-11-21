# EasyRemote

一个自研的**跨平台远程控制**项目，覆盖服务端（Go）、Rust 核心引擎、Flutter 客户端与 React 运营后台。

> 目标：低延迟、可自托管的远程桌面。协议、错误码与接口契约全部以 `docs/spec/` 为唯一来源。

## 架构总览

| 目录 | 职责 | 技术栈 |
|---|---|---|
| `server/` | 网关（`/v1/*`）、运营后台（`/admin/v1/*`）、rendezvous、UDP 中继、stack 聚合入口 | Go |
| `core/` | 传输 / 采集 / 编解码 / ABR / 输入注入 / 会话编排，并通过 FFI 暴露给客户端 | Rust |
| `client/` | 用户客户端 | Flutter |
| `web-admin/` | 运营与客服控制台 | React + TypeScript |
| `docs/spec/` | **接口契约**（`contract.yaml`）、错误码、协议、页面说明 | — |
| `docs/prd/` | 现行产品需求（v0.8 为当前版本） | — |
| `scripts/` | 代码生成、闸门、跨机验证脚本 | Bash / Node |
| `design/` | 设计 token 与生成物 | — |
| `prototype/` | 交互原型（纯静态页面） | — |

## 快速开始

### 服务端

```bash
cd server
go build ./...
go run ./cmd/stack -addr :18090      # 内存存储 + 信令，本地起整套服务
go test ./...
```

可选环境变量：

| 变量 | 说明 |
|---|---|
| `DATABASE_URL` | 设置后使用 PostgreSQL，否则用内存存储 |
| `REDIS_URL` | 设置后使用 Redis 做限流 / 中继注册 / 信令 |
| `ER_SECRET` | 生产环境必须设置；开发环境留空时生成临时密钥 |

### Rust 核心

```bash
cd core
cargo build --workspace
cargo test --workspace
```

### 客户端

```bash
cd client
flutter pub get
flutter run -d chrome --dart-define=ER_GATEWAY=http://127.0.0.1:8090
```

### 运营后台

```bash
cd web-admin
npm install
npm run dev
```

## 开发约定

本仓库对实现有强约束，详见 [AGENTS.md](AGENTS.md)。要点：

- 响应只能由 `httpx.OK` / `httpx.OKPage` / `httpx.Err` 产出，禁止裸写 `map[string]any`
- 改接口先改 `docs/spec/contract.yaml`，再跑 `node scripts/gen-contract.mjs`
- 错误码只能从 `errcode` 选取，新增码先改 `docs/spec/error-codes.md`
- 字段命名一律 `snake_case`；`code` 这个键全站只归信封所有

提交前跑闸门：

```bash
bash scripts/gate.sh
```

## 文档

- [产品需求 PRD v0.8](docs/prd/easyremote-prd-v0.8.md)
- [接口契约](docs/spec/contract.yaml) ｜ [错误码](docs/spec/error-codes.md) ｜ [协议](docs/spec/protocol.md) ｜ [页面](docs/spec/pages.md)
- [竞品调研](docs/research/uu-remote-analysis.md)

## 说明

- 仓库中的 `docs/impl/` 保留了各轮评审与真机验证证据，其中的主机地址/机器名已脱敏为占位符。
- 演示账号、验证码等均为测试数据，不对应任何真实账号。

## 许可证

[MIT](LICENSE)
