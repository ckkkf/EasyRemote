# ER-011 证据：response:any 清零与三端 DTO

- 状态: 完成（`bash scripts/gate.sh` → GATE: PASS）
- 日期: 2026-11-22

## 1. 结果

- `grep 'response: any' docs/spec/contract.yaml` → **0 条**。
- 新增 18 个响应类型（Login/DeviceList/VerifyCode/Connect/Consent/SessionState/SessionKeys/RelayTicket/EmailCode/EmailLogin/FileUpload/AssistCreate/AssistRedeem/Reflect/ClientConfig 等）。
- 服务端 gateway/appconfig 全部返回 `contract.*` 类型，不再拼 `map[string]any` 响应。
- 生成器修复 `map<X>` 语义：Go `map[string]X`、TS `Record<string,X>`、Dart `Map<String,X>`（此前错误生成 map[bool]string / Record<bool,string>）。
- 新增 `scripts/tests/contract-response.test.mjs` 3 条测试。

## 2. 字段映射（端点 → 类型）

| 端点 | 类型 |
|---|---|
| GW-01/09/20 | OkResult |
| GW-02 | LoginResult{token,account_id} |
| GW-03 | DeviceListResult{devices:list<DeviceItem>} |
| GW-05 | VerifyCodeResult{code,expires_at} |
| GW-06 | ConnectResult{session_id,state,trusted} |
| GW-07 | ConsentResult{session_id,state,auth_expire_at} |
| GW-08 | SessionStateResult{session_id,state,reason} |
| GW-10 | SessionKeysResult{session_id,controller_id,target_id,controller_pubkey,target_pubkey} |
| GW-11 | RelayTicketResult{ticket,self_device} |
| GW-12/13 | EmailCodeResult{ok,email} / EmailLoginResult{token,account_id,email} |
| GW-14 | FileUploadResult{file:FileRow} |
| GW-15 | binary（octet-stream） |
| GW-16/17 | AssistCreateResult / AssistRedeemResult |
| GW-18 | ClientConfigResult{config:ClientRuntimeConfig} |
| GW-19 | ReflectResult{ip,port,source,via,at_unix} |

`ClientRuntimeConfig` = session/file/diag/client 四组限制 + `capabilities: map<bool>`（只读能力表）。

## 3. 验收断言 → 测试 → 结果

| 断言 | 测试 | 结果 |
|---|---|---|
| 完成态没有 response:any | `TestContractHasNoAnyResponse` | PASS |
| 每个端点三端都有 DTO 且字段齐全 | `TestEveryEndpointResponseHasConcreteDTO` | PASS |
| binary 下载在契约中有明确语义 | `TestBinaryResponseIsDocumented` | PASS |

> 字段级检查覆盖每个 endpoint 的 response 类型及其全部字段，读取的是实际生成物（Go/TS/Dart），不是手写副本。

## 4. 命令与输出

```
$ node --test scripts/tests/*.test.mjs
19 tests pass（含本卡 3 条）

$ node scripts/gen-contract.mjs --check
✓ 三端契约一致（12 个类型 → 现 30+ 个类型 / 32 条接口）

$ cd server && go test ./internal/gateway/ ./internal/appconfig/
ok

$ bash scripts/gate.sh
  scripts PASS / contract PASS / go PASS / rust PASS / flutter PASS / web PASS
GATE: PASS
```

## 5. 遗留与边界

- 客户端/后台尚未全面改用生成的 DTO（仍以动态 map 解析为主）；生成类型已可用，切换归 **ER-014**。
- 代码支持 `GET /v1/files` 列表，但契约只登记 POST /v1/files；method 语义由 **ER-013** 补齐（ER-010 checker 的 path-only 注册无法表达同路径多方法）。
- GW-15 返回字节流，客户端下载实现必须按 octet-stream 处理，不能套 JSON 信封。
- `ClientRuntimeConfig` 的 capabilities 依赖 `capability.Public()`；新增能力必须改台账并重新生成。

## 6. 回滚

- 回滚契约/生成器/服务端响应即可；回滚会重新引入 response:any，属于完成态退化，不得作为长期方案。
- 生成物禁止手改；回滚通过 `git revert` + `node scripts/gen-contract.mjs`。
