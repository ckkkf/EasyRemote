# EasyRemote · 实现铁律

> 这份文件是给 AI（以及人类）的硬约束。开工前必读，改完必须跑闸门。
> 冲突时以本文件为准；本文件与 docs/spec 冲突时，先改本文件再动代码。

## 1. 只准读这些

- 现行 PRD：`docs/prd/easyremote-prd-v0.8.md`（v0.1–v0.7 已过期，禁止作为实现依据）
- 契约：`docs/spec/contract.yaml`（接口形状的唯一来源）、`docs/spec/error-codes.md`、`docs/spec/protocol.md`、`docs/spec/pages.md`
- 当前任务卡：`docs/impl/tasks/<ID>.md`
- `docs/impl/review-round*.md`、`final-status.md` 只是历史记录，不是需求。

## 2. 目录职责

| 目录 | 职责 | 约束 |
|---|---|---|
| `server/internal/gateway` | C 端 HTTP（`/v1/*`） | 响应只能走 `httpx.OK` / `httpx.OKPage` / `httpx.Err` |
| `server/internal/adminhttp` | 后台 HTTP（`/admin/v1/*`） | 同上 |
| `server/internal/adminapi` | 后台业务 | 返回具体类型，禁止 `map[string]any` |
| `server/internal/errcode` | 错误码与对外文案 | 码表来自 `docs/spec/error-codes.md`，禁止手改生成物 |
| `server/internal/httpx` | 响应信封、中间件、解码 | 全站唯一的写响应入口 |
| `server/internal/contract` | **生成物** | 由 `scripts/gen-contract.mjs` 生成，禁止手改 |
| `client` | Flutter 客户端 | 错误只认 `Envelope`（`code/name/message/trace_id`） |
| `web-admin` | React 后台 | 同上 |

## 3. 铁律

1. **响应只能由 `httpx.OK` / `httpx.OKPage` / `httpx.Err` 产出**。成功体形状固定为 `{code:0, name:"OK", data, trace_id}`，错误体为 `{code, name, message, retryable, trace_id}`。
2. **禁止**：`json.NewEncoder` 直接写响应、`http.Error`、`w.Write([]byte(...))`、用裸 `map[string]any` 作为响应体。
3. **改接口先改契约**：先改 `docs/spec/contract.yaml`，跑 `node scripts/gen-contract.mjs`，再写实现。顺序反了就是漂移。
4. **错误码只能从 `errcode` 选**，新增码先加到 `docs/spec/error-codes.md` 再跑 `node scripts/gen-errors.mjs`。禁止自造数字。
5. **每条路由必须有契约测试覆盖**（`server/internal/adminhttp/contract_test.go` 的表来自生成物），否则不算完成。
6. **字段命名一律 snake_case**；`code` 这个键在全站只归信封所有，业务里的验证码叫 `verify_code`。
7. **不许改 `docs/spec/*` 去迁就实现**。spec 与实现不一致时，实现错。
8. **过渡只允许在「同语义的合法端点」之间插值**。所有线性插值器（`Color.lerp`、`AnimatedContainer`、`Tween` 驱动的隐式动画）都是对每个分量**独立**线性插值，会经过端点之外的中间态，而那个中间态往往根本不是合法状态。因此：
   - 淡出只动 **alpha / opacity**，不动颜色分量。要「变成透明」时，目标色必须是与背景**同 RGB、alpha=0** 的色值；**禁止 `Colors.transparent`**（它是透明**黑**，中途 RGB 会一起塌向 0，表现就是闪一下变暗/发灰）。
   - 同理禁止把尺寸、偏移、圆角、亮度 tween 到 0 或负值来表达「没有」：先把「无」显式建模成一个合法状态，再在合法状态之间过渡。
   - 判据：过渡路径上的任意一帧，都必须是本来就可能出现的状态；出现不了的中间态就是 bug。
9. **同一视觉属性只能有一个来源**。底色、边框、圆角、阴影、悬停/按下反馈：要么全交给框架原生（`Material.color` + `InkWell.hoverColor`/水波），要么全自己画。**禁止「关掉原生反馈、再自己补一层」**——两层叠加时，上层淡出的那几帧会把被盖住的下层「露出来」（闪烁/跳色），而且两层的时序、缓动、圆角各写一遍，必然漂移。
10. **UI 修复的默认方向是减代码**。遇到视觉 bug 先问「能不能删掉自定义样式、改用原生组件」，禁止用新的自定义样式去盖旧的自定义样式。UI 补丁净增行数为正时，必须在回复里说明为什么不能删。
11. **视觉修复必须给可复现的判据**：像素采样、几何测量、组件属性断言，任一即可；「看起来好了」不算验证。测试要守**不变量**（例：「底色必须不透明」「该属性只有一个来源」），不要守当时的实现细节（例：「某个 overlayColor 等于透明」）——后者会把错误方案固化成回归。

## 4. 完成任务的定义

- `bash scripts/gate.sh` 全绿（默认即严格模式）
- 任务卡里每条验收断言都有对应测试名
- 回复里必须贴出：改了哪些文件 + 每个响应字段来自 spec 哪一行 + gate 输出
- 没贴 gate 输出就说「已完成」的，视为未完成

## 5. 工作流

```
读任务卡 → 输出字段映射表（字段 ← spec 行号）→ 改 contract.yaml → gen-contract → 写实现 → 写测试 → bash scripts/gate.sh → 贴输出
```

字段映射表缺一行就停下来问，不要猜。

## 6. 常用命令

```bash
bash scripts/gate.sh                    # 严格闸门（默认；任何一步红都不算完成）
bash scripts/gate.sh --report           # 报告模式（只列待办，不判红）
bash scripts/gate.sh --update-baseline  # 更新禁止模式基线，需在 PR 里说明
node scripts/gen-contract.mjs        # 生成三端契约
node scripts/gen-contract.mjs --check
node scripts/gen-errors.mjs --check
```
