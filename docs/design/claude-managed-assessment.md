# Claude Code 托管模式独立评估

评估日期：2026-10-25。基准 HEAD：`aff519bf1a0d5f30929543f2be8af435e8d4b7ca`，结合调查期间的工作区代码。仓库存在并发修改；行号可能随后移动，因此同时给出函数名。本文只评估 Claude 托管路径，不把主线新增的 Codex steer、permissions、计划/diff/用量展示当作 Claude 已具备的能力。未编辑实现代码，也未回退他人改动。

“证实”指代码分支可以直接推出的行为，或现有测试明确覆盖的行为；不代表已经用真实 Claude CLI 端到端复现。“待验证”指触发条件、外部协议或实际影响仍需实测。优先级：P1 为消息可靠性或执行控制问题，P2 为能力、可观测性及展示缺口。本次没有足够证据认定 P0。

## 结论与已具备能力

默认托管模式已经采用长驻进程，执行中追加消息会写入 CLI；主要问题是消息交付的持久性、后台审批的停止控制，以及配置生效时机。无需据此重做整个托管架构。

- `internal/server/runtime.go:88` 的 `defaultProcessStarter` 默认调用 `claude.StartProcess`；单轮 `claude.Run` 是 Runner/`CC_REMOTE_ONESHOT` 回退路径。
- `internal/claude/process.go:50`、`:207`：启用 `--input-format stream-json` 和 `--replay-user-messages`，通过同一 stdin 发送后续消息。`internal/server/runtime_test.go:130` 的 `TestMessagesUseClaudeQueue` 覆盖多消息及合并回显，但使用 fake process。
- `internal/claude/run.go:208` 与 `internal/claude/bridge.go:70`：AskUserQuestion、ExitPlanMode 使用 PreToolUse hook；普通权限提示通过 MCP approval。桥接失败默认拒绝。不能判定为“没有审批支持”。
- `internal/store/stats.go:41` 已对累计成本及 modelUsage 做差分；`internal/store/stats_test.go:11` 覆盖恢复、重置与重复权限拒绝结果。不能判定为“每轮直接累加累计成本”。
- `internal/server/runtime_test.go:19` 覆盖空闲回收后 resume，`:202` 覆盖第二次停止关闭进程；`internal/store/store_test.go:191` 覆盖重启后标记 interrupted、过期 pending。

## 最重要的三项证实缺陷

### C1 · P1 · 已接受、未回显的消息没有持久化保证

**证据：** `internal/server/runtime.go:231` 的 `enqueueLocked` 仅把消息放进内存 `rt.sent`；`:273` 的 `replay` 才调用 `recordLocked` 落库，另有 `turnOutputLocked` 在首次输出时提前记录。`internal/server/server.go` 的 `message` 返回 HTTP 202。`internal/store/store.go:180` 的启动恢复只标记旧执行中断及交互过期，没有恢复消息队列。`internal/server/sends.go` 的 sendId 去重也仅在内存中，存在一小时和数量上限。

**确定影响：** 宿主在接收后、落库前突然退出，网页已获接受响应的消息可能没有 CC Remote 历史记录，也没有可恢复队列。若请求已送达但 HTTP 响应丢失，跨宿主重启的相同 sendId 重试没有去重保证。不能进一步断言 CLI 一定没收到或一定执行了：这属于交付不确定性。

正常进程退出有 `processExit`（`internal/server/runtime.go:395`）将残余消息补入历史；这不覆盖宿主硬退出。且 `rt.stopping` 为真时不附加“排队消息未执行”的专门提示，历史里的普通 user 记录不足以表达交付状态。

**落地建议：** 在返回 202 前持久化消息及 sendId，区分 accepted、sent、acknowledged、delivery-unknown；重启后展示待核对消息，不自动重放可能已执行的请求。验收至少覆盖接受后未写管道、已写未回显、响应丢失后重启重试。这是可靠性改进，不能只靠重启时补一条错误提示完成。

### C2 · P1 · 只有后台审批等待时，“停止”会空操作

**证据：** `internal/server/runtime.go:566` 的 `bridgeRuntimeLocked` 为轮次之间的后台审批增加 approvals 并调用 `busyLocked`，因此会有 run。`:431` 的 `interruptLocked` 却先判断 `!rt.turn && len(rt.sent) == 0` 并返回，后面的拒绝 pending、发送 interrupt 和超时终止均不会发生。`internal/server/server.go:788` 的 `stop` 只检查 run 是否存在，随后仍返回 200 / `stopping`。

**确定触发：** 主轮次已完成、队列为空，后台任务发起独立审批，用户点击停止。`internal/server/runtime_test.go:226` 的 `TestBridgeRequestWhileIdle` 已搭建这种审批状态，但没有测试对它调用 stop。这是代码可确定的控制缺口，尚未用真实 CLI 复现。

**落地建议：** 优先作为小型明确 bug 修复：停止判断应包含审批等待状态，至少取消该会话未决交互并让调用方收到拒绝；明确是否还要终止后台任务。增加“idle → background approval → stop”的定向回归测试，并验证 API 返回与实际状态一致。无需为此启动架构迁移。

### C3 · P1 · 模型/权限配置不保证“下一轮生效”

**证据：** `internal/server/runtime.go:105` 的 `runtimeLocked` 比较 launchKey，但在 `rt.run != nil`、进程尚在启动或 `BackgroundTasks() > 0` 时直接复用旧 runtime。消息已经写入该进程后，也没有在下一轮边界重新检查配置的路径。提示却在 `internal/server/commands.go:395` 和 `web/src/SessionSettings.tsx:106` 明确写“下一轮生效”。

**确定影响：** 执行中修改配置并追加消息，下一轮仍可使用旧启动参数；有后台任务时，主轮次结束后发送的新消息也可沿用旧参数。模型和 effort 的变化被推迟，收紧 permission mode 也不能保证立刻约束下一轮。不是所有改配置都会失败：无运行轮次、无后台任务的闲置进程可被替换。

**落地建议：** 小改可先修正文案，展示“已保存、等待可安全重启”及实际运行配置；这只能解决误导，不能兑现权限收紧。完整修复需要在允许后续轮次运行前应用配置并确认，或明确阻止继续发送；不要为应用设置静默杀死后台任务。增加繁忙时改设置、后台任务存活时改设置的测试。

## 其他证实的缺口

| 编号 / 优先级 | 证实范围与代码证据 | 建议 |
| --- | --- | --- |
| C4 / P2：托管计划审批体验与记录不完整 | `web/src/App.tsx` 的 `shared` / `linked` 仅对 terminal 启用；托管 pending 使用 `RequestCard`（调查后期约 `:959`），计划显示原始 JSON，只能允许本次/拒绝。`internal/server/server.go` 的 `resolve` 拒绝托管回答中的 Mode。`web/src/QuestionCard.tsx:68` 的历史计划只读 `call.input.plan`，没有从审批快照或成功结果补正文。因此原始 tool input 为空的计划，审批后可显示为空计划；并非所有计划都必然为空。 | 托管模式复用计划专用 UI，持久化获批计划快照、决定及反馈。批准后权限模式需要真实运行时支持，不能只增加按钮。 |
| C5 / P2：审批来源及决策记录不足 | `internal/claude/bridge.go:152` 的 schema 声明 tool_use_id，但 `tools/call` 的解析结构（`:154`）不接收它；bridgeRequest（`:18`）也不携带调用 ID 或父任务 ID。`resolve` 只在 terminal connection 分支写 interaction outcome 事件。普通 MCP 拒绝响应（`:168`）使用固定理由，未透传 answer.Message。 | 贯通 tool-use / parent-task ID，并记录决定、原因及关联输入。这是可追溯性缺口；尚无证据证明已发生错批或越权。 |
| C6 / P2：工具与子 agent 展示并非完整事件视图 | `internal/claude/run.go:250` 的 stream_event 只保留主 agent text_delta；`:321` 的 contentEvents 只处理 text、tool_use、tool_result。工具参数增量、thinking 未进入该展示管线。启动参数（`:42`，`internal/claude/process.go:50`）未启用 forward-subagent-text，虽已有 parent 归属和 AgentCard。 | 优先展示工具开始与参数生成状态、保留可展示的结构化结果；按目标 CLI 能力启用子 agent 转发。thinking 是否展示需另定产品范围。官方提供工具流增量及子 agent 转发能力，见 S1、S5。 |
| C7 / P2：托管运行态配置与上下文展示不足 | `internal/server/channels.go:41` 的 decorate 只为 terminal 填充 live；`web/src/SessionSettings.tsx:47` 的 live chips 也只读取 terminal。托管 init/result payload 虽存储，却没有形成同等的当前模型、权限、上下文展示。 | 区分配置期望、CLI 已报告值与未知；有报告才展示上下文占比，避免从累计 token 推算上下文。 |
| C8 / P2：用量“已知”判断过宽 | `internal/store/stats.go:82` 以 Results > 0 设置 CostKnown，而 COUNT(r) 并不要求 payload 存在有效 total_cost_usd；usageCTE 又过滤 cost > 0，并只从 modelUsage 取 token。缺失成本字段的 result 可以得到“已知零费用”；有 usage、无 modelUsage 时 token 无对应回退。 | 分别判断成本/token 字段是否存在且有效，保留真实零值。已有累计差分应保留；用量为 API 价格估算，不能等同订阅额度。 |

审批覆盖还有明确的外部边界：普通 permission prompt 只覆盖 CLI 交给它的请求，不等于每次工具调用都必须网页确认。官方 S3 说明权限规则及模式参与决策；S1 还明确 requiresUserInteraction MCP 工具不能由 permission-prompt-tool 的 allow 批准。因此不能承诺“所有 MCP 交互均可在网页批准”。仓库的 AskUserQuestion / ExitPlanMode hook 方案本身与官方交互机制相符，不应整体判错。

## 待验证项：不能写成已发生缺陷

| 项目 | 现有依据、猜测及所缺证据 |
| --- | --- |
| 回显是否会导致错配或卡住 | `runtime.go` 的 replayCount 按文本匹配，完全不匹配时仍消费第一条；`process.go:141` 仅接受 string content 的 isReplay。存在脆弱性，但未证明真实 CLI 会输出不符合当前假设的回显。官方 S1 说明有 acknowledgment，未在本次核对中确立“合并并以换行连接”的完整契约。需要固定版本的真实协议样本。 |
| 旧进程尾部事件污染新生命周期 | runtimeLocked 异步关闭旧进程并启动新进程；processEvent 对 Claude 通常没有当前 runtime 身份检查。可能存在尾部输出交错，但尚无时序复现，不能断言已经串轮或覆盖状态。 |
| 非终结错误后永久忙碌 | `process.go:141` 读取 parseEvent 返回的 turnErr，仅在 terminal 时传 Result；`run.go:292` 的独立 error 属非 terminal。如果 CLI 报这种错误后保持存活且不再给 result，可能无法结算。缺实际 CLI 序列，暂列假设。 |
| 订阅用量接口长期兼容性 | `internal/claude/usage.go` 读取 OAuth 凭据并请求 `/api/oauth/usage`，server 有缓存、过期和 429 退避；本次官方文档没有建立这个具体端点的稳定公开契约。不能据此宣称当前不可用或违规，需单独验证版本与账号类型。 |
| 导入历史后的费用归属 | stats 第一个累计值全计入首次结果日期；S4 说明 resume 可携带先前消费。若导入记录不含累计基线，旧消费可能被归到导入后的日期。需带已知消费的导入样本验证；不是已证明的普遍重复计费。 |
| 恢复是否依赖缺失的原生历史 | 现有代码恢复会话上下文依赖 --resume，并不是把 SQLite 展示事件重新注入 CLI。原生历史被删除/迁移后的具体失败提示、权限及后台任务恢复效果未做真实验证。S6 只保证会话历史语义，不保证进程、工具或文件系统回滚。 |

执行中追加输入应描述为“已接通输入管道、按当前实现排队后续轮次”，不能承诺立即改变正在执行的工具。停止后保留排队消息也是当前明确产品行为，不单独当作 bug。主线新增 steer 通过 Codex provider 门禁，不改变本报告的 Claude 判断。

## 已执行验证与限制

- `go test ./internal/claude ./internal/server ./internal/store`：通过；server 输出约 51.6 秒，claude 首次命中缓存。
- `go test -count=1 ./internal/claude ./internal/store`：通过，claude 约 2.15 秒、store 约 1.09 秒。
- `node --experimental-strip-types --test web/tests/toolGroups.test.mjs web/tests/sharedQuestions.test.mjs web/tests/sessionInformation.test.mjs`：40 项通过，0 失败。
- Go process 测试使用测试二进制模拟 CLI；server queue 测试使用 fakeProc。前端上述测试验证数据整形/渲染，不等于完整网页与真实 CLI 的集成验证。既有前端测试会生成 `web/node_modules/.cache` 测试构建缓存；本次唯一交付文件为本文，未新增测试或产品实现。
- 共享工作区在调查期间出现其他修改，包括 server/runtime/App 与 Codex 功能；已检查其 provider 门禁，未回退。测试通过仅代表测试执行时的工作区，不能外推到并发修改后的所有状态。按要求收敛后未追加检索或测试。

## 官方来源

以下资料均于 2026-10-25 查阅；外部能力描述以文档为依据，具体已安装 CLI 版本兼容性仍需另验。SDK 文档用于说明能力及语义，不等于本仓库手写 Go 协议已自动获得这些保证。

- **S1**：[Anthropic Claude Code CLI reference](https://code.claude.com/docs/en/cli-reference)：stream-json、replay-user-messages、resume、permission-prompt-tool、forward-subagent-text 及交互型 MCP 限制。
- **S2**：[Anthropic Hooks reference](https://code.claude.com/docs/en/hooks#tools-that-require-user-interaction)：AskUserQuestion/ExitPlanMode 的 updatedInput；同页 ExitPlanMode 输入说明计划由 CLI 从文件注入 hook，模型原始 tool_input 通常为空。
- **S3**：[Anthropic Configure permissions](https://code.claude.com/docs/en/agent-sdk/permissions)：权限规则、模式与运行时批准的关系，不能将网页批准等同于所有工具的总闸门。
- **S4**：[Anthropic Track cost and usage](https://code.claude.com/docs/en/agent-sdk/cost-tracking#track-costs-in-streaming-input-mode)：streaming 中 usage 与 total_cost_usd/modelUsage 作用域不同，后两者为累计值，resume 可带入旧消费。
- **S5**：[Anthropic Stream responses in real-time](https://code.claude.com/docs/en/agent-sdk/streaming-output#stream-tool-calls)：工具 content_block_start、input_json_delta 与 content_block_stop。
- **S6**：[Anthropic Work with sessions](https://code.claude.com/docs/en/agent-sdk/sessions)：持久化对话与恢复语义；会话恢复不等于文件系统恢复。
- **S7**：[Anthropic Streaming Input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode)：持久输入、消息排队与中断能力；不能据此额外推定具体 CLI 回显文本格式。

## 本轮评估后的跟进

- C2 已修复：`interruptLocked` 不再忽略单独存在的审批等待；停止会拒绝未决审批并结算。新增 `TestStopDeniesBackgroundApprovalBetweenTurns` 验证实际 HTTP 审批桥返回拒绝及会话中断；这不等于承诺所有后台任务都被终止。
- C3 已修正网页和命令反馈文案：配置在当前工作及后台任务结束后，下次发送时应用，不再保证繁忙进程的下一轮自动切换。没有改变现有后台任务保护策略，也没有实现动态权限收紧。
- C1 与 C4–C8 保留后续处理；尤其是消息持久化及送达不确定性需要单独设计，不能通过自动重放解决。
