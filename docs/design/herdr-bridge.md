# 设计：Herdr 桥接（ccr 向 Herdr 上报 agent 状态）

状态：已实现，待在 Herdr 里实测（2026-11-13）。范围：仅 Claude Code 终端会话；Codex 暂不做。

实现：`internal/terminal/herdr.go`（桥）、`attach.go`（接入点）、`herdr_test.go`（假 Herdr 二进制记录调用）。与设计的差异：轮询用 `GET /api/sessions` 而不是 `GET /api/sessions/{id}`（后者会标记「已查看」并带事件）；第一版没有 `--agent-session-id` 和 `--token`；失败后退避 10 秒再试，避免坏掉的 herdr 每秒被拉起一次。阶段 0 的三个假设尚未验证（见下）。

**2026-11-13 补充：本机装的是另一代 Herdr。** `herdr-app` + `herdr-core`（命令行 `hc` 0.1.21）没有 `pane report-agent`，状态来自程序自己输出的 OSC 7501 序列（Program Status Protocol：`ESC ] 7501 ; state=working:app=claude-code ESC \`）。对照实验：安装版 `ccr` 在 `hc` 终端里 `status` 为空、`reported=false`，Claude 工作期间也没有任何状态——宿主的 PTY（Windows 为 ConPTY）挡住了 Claude 自己的上报，也没人替它上报。因此新增后端 `internal/terminal/osc7501.go`：`ccr` 在 `hc` 终端（环境有 `HC_TERMINAL_ID`）里往自己的 stdout 写 OSC 7501，状态映射与上文相同（`blocked` 带 base64 的 `msg`，离线或退出时 `state=clear`）。写入只发生在屏幕输出的序列边界，避免插进被切断的转义序列或 UTF-8 字符中间。实测（`hc get`）：`idle → working → idle`（`turn` 递增）、`blocked`（msg=等待回答）、网页回答后约 1 秒回到 `working`、之后 `idle`。两个后端共用同一个轮询（`statusSink`），终端版 Herdr 仍走 `pane report-agent`。

## 背景与问题

[Herdr](https://herdr.dev) 是终端里的 agent 复用器：在 workspace / tab / pane 里并排运行多个 agent，侧栏显示每个 agent 的状态（`working` / `blocked` / `idle` / `done`），agent 停下来等回答时特别提示。

在 Herdr 的 pane 里运行 `ccr` 能正常使用，但 Herdr 的 agent 功能失效，原因有三：

1. Herdr 官方的 Claude 集成是 `SessionStart` hook，只在 `HERDR_ENV=1` 且有 `HERDR_PANE_ID` 时上报。Claude Code 由宿主在自己的 PTY 里启动，继承宿主环境，没有这些变量，hook 什么都不做。
2. pane 的前台进程是 `ccr`（attach 客户端），不是 `claude`，按进程名识别不到 agent。
3. Herdr 的状态靠屏幕识别。cc-remote 的「等待操作」可能由网页触发或由 hook 驱动，屏幕上不一定有对话框。

结论：Claude Code 的真实状态在宿主里（hook + transcript，比屏幕识别精确），而 Herdr 提供了公开的上报接口。缺的是中间一座桥。

## 目标与非目标

目标：

- 在 Herdr pane 里 `ccr` 接入某个会话期间，Herdr 把该 pane 识别为 Claude agent，并显示准确的 `working` / `blocked` / `idle`。
- 在手机或网页上回答了待答请求后，Herdr 侧栏的 `blocked` 随之消失。
- Herdr 重启恢复布局时，自动用 `ccr` 接回同一个会话。
- 不在 Herdr 里运行时，行为与现在完全一致。

非目标：

- Codex（`ccr` 目前没有 Codex 终端会话，另立项目）。
- Herdr → cc-remote 的反向同步（订阅 pane 事件、网页显示 Herdr pane）。
- 由 cc-remote 主动创建 Herdr pane。
- 修改宿主、网页或 Herdr 的 Claude 集成。

## 总体结构

```
 cc-remote 宿主 ── GET /api/sessions/{id} ──▶ ccr attach 客户端 ── herdr pane report-agent ──▶ Herdr
 (hook + transcript 状态)   (轮询)           (运行在 Herdr pane 里，有 HERDR_PANE_ID)
```

桥放在 attach 客户端，不放在宿主，因为：

- pane 的生命周期等于 attach 的生命周期。宿主里的 Claude 进程可能同时被网页和多个 pane 共用，宿主不知道该向哪个 pane 上报。
- 客户端已经持有宿主地址、令牌和 HTTP 客户端（`client.do`），不需要新增宿主接口。
- Herdr 需要的环境变量天然在客户端进程里。

## 详细设计

### 1. 启用条件

attach 开始时检查环境：`HERDR_ENV=1`，且 `HERDR_PANE_ID`、`HERDR_BIN_PATH` 非空。任一缺失则整个桥不启动，不产生任何输出或错误。`CC_REMOTE_HERDR=0` 可显式关闭。

### 2. 上报方式

调用 `"$HERDR_BIN_PATH" pane report-agent "$HERDR_PANE_ID" …`，不直接连 socket。理由：文档把 CLI 作为跨平台（含 Windows 命名管道）的稳妥路径；状态变化稀疏，每次起一个子进程的开销可以忽略。

规则（来自 Herdr 的 [add-herdr-support](https://herdr.dev/docs/add-herdr-support/)）：

- 每次调用带短超时（2 秒），在后台协程里执行，失败只记日志，不影响终端。
- 只发最新状态：状态变化排队时丢弃旧的。
- `--source custom:cc-remote`（不能以 `herdr:` 开头），`--agent claude`。
- `--seq` 用 Unix 毫秒时间戳，跨会话、跨重启单调递增。
- `--agent-session-id` 用 Claude 会话 ID（`claudeSessionID`），便于 Herdr 关联。

### 3. 状态映射

| cc-remote（`Status` / `Online`） | Herdr | 说明 |
|---|---|---|
| `running` | `working` | |
| `waiting` | `blocked` | `--message` 见下 |
| `idle`、`interrupted` | `idle` | |
| `Online=false`（宿主重启、进程退出） | `release-agent` | 重新在线后再 `report-agent` |
| 客户端脱离（`Ctrl+]`）、会话结束、客户端退出 | `release-agent` | 释放后 pane 回到普通 shell |

`blocked` 的 `--message`：第一版固定为「等待回答」；后续可改为待答请求的工具名或问题摘要（需要宿主列表接口带上，见开放问题）。

`done`（空闲且未查看）：CLI 文档只列了 `idle` / `working` / `blocked`。先上报 `idle`，由 Herdr 自己根据 `working → idle` 的跳变生成完成提示；实测不满足再评估。

### 4. 状态来源

在 `attachOnce` 里再起一个协程，每 1 秒 `GET /api/sessions/{id}`（与现有 `client.recent` 用的会话列表同源），取 `status` 和 `online`。值与上次相同则不上报。1 秒与宿主 transcript 同步节拍一致；不新增宿主接口、不改 WebSocket 协议。

连接断开重连（`attach` 外层循环里宿主重启）时：释放旧状态，重连成功后重新上报当前状态。

### 5. 展示元数据

首次上报后用 `pane report-metadata` 补一次展示信息（`--source user:cc-remote`，不影响生命周期）：

- `--title` 会话标题。
- `--display-agent` `Claude`。
- `--token model=<模型>`（若列表接口提供）。

元数据失败同样只记日志。

### 6. 恢复

`report-agent` 带 `resume_argv`：`["ccr", "<会话 ID>"]`。Herdr 服务重启并恢复布局时会在该 pane 重跑此命令，`ccr` 接回宿主里同一个会话（会话已持久化，宿主离线时 `ccr` 自带等待和 `--resume` 恢复）。

约束（Herdr 文档）：首个元素必须是裸命令名；需要 Herdr 0.9.2 或更高；该 source 必须已持有 pane，所以 `resume_argv` 与首次 `report-agent` 同时发出。

### 7. 退出清理

`attachOnce` 的 `defer` 里执行 `release-agent`（带 `--source`、`--agent`、`--seq`），最多等 500 毫秒，与现有 detach 逻辑的超时一致。进程被强杀时 Herdr 在 pane 回到 shell 提示符后会自行清除，这是 Herdr 的兜底。

## 代码改动范围

| 文件 | 改动 |
|---|---|
| `internal/terminal/herdr.go`（新） | 环境检测、`report-agent` / `release-agent` / `report-metadata` 的命令封装、最新状态合并、后台发送 |
| `internal/terminal/attach.go` | `attachOnce` 里启动桥协程、`defer` 里释放 |
| `internal/terminal/herdr_test.go`（新） | 用假的 `HERDR_BIN_PATH`（记录参数的小程序）测启用条件、映射、去重、序号、释放、失败不影响 |
| `docs/usage.md`、`README` | 增加「在 Herdr 里使用」一节 |

宿主、网页、store、API 均不改。

## 失败处理

- Herdr 二进制不存在、超时、非零退出：忽略，记 debug 日志，下次状态变化重试。
- 宿主不可达：沿用现有「宿主重启，恢复后自动接入」逻辑，桥此期间释放状态。
- 同一会话在多个 pane 接入：各自独立上报同一状态，互不影响。

## 验证

分两步，先小后大：

**阶段 0：手工验证三个假设（不写代码）。** 在 Herdr 的 pane 里手动执行 `herdr pane report-agent`，并运行 `ccr`：

1. 对前台进程是 `ccr` 的 pane 上报后，Herdr 的 `herdr agent list` 是否显示该 agent，状态是否压过屏幕识别的结果（文档有「权威」概念但优先级未写明）。
2. `working → idle` 是否会被 Herdr 当作「完成未查看」。
3. Windows beta 上 `HERDR_BIN_PATH`、命名管道、ConPTY 转发能否一起工作。

阶段 0 任一项不成立，则调整方案后再实现。

**阶段 1：实现与验收。**

- 单元测试：上面 `herdr_test.go` 的各项。
- 手动验收：
  1. Herdr pane 里 `ccr`，侧栏出现 Claude agent，发消息时 `working`，结束后 `idle`。
  2. 触发一个工具审批，侧栏变 `blocked`；在手机网页上批准，侧栏恢复 `working`。
  3. `Ctrl+]` 脱离，agent 标记消失；再 `ccr` 接回，标记恢复。
  4. `herdr session stop` 后重启，pane 自动重跑 `ccr`，接回同一会话。
  5. 不在 Herdr 里运行 `ccr`，行为不变，无多余输出。

## 开放问题

1. 列表接口目前是否返回待答请求的摘要和模型名，决定第 3、5 节能否在第一版带上；不能则第一版用固定文案，另开任务扩展接口。
2. 轮询 1 秒是否足够及时；若 Herdr 侧感觉延迟，可改成宿主推送（WebSocket 文本消息），但那需要改协议。
3. Linux / macOS 上 `HERDR_BIN_PATH` 是否总是绝对路径；若不是，需要回退到 `PATH` 里的 `herdr`。

## 风险

| 风险 | 影响 | 应对 |
|---|---|---|
| Herdr 的权威规则下，屏幕识别覆盖了上报 | 状态不准 | 阶段 0 验证；必要时用 `pane.clear_agent_authority` 或调整上报来源 |
| Herdr 升级改了 CLI 参数 | 上报失败 | 失败静默；`resume_argv` 需要 Herdr ≥ 0.9.2，低版本忽略但状态仍可用 |
| 每次状态变化起子进程 | 轻微开销 | 状态稀疏；只发最新值 |
