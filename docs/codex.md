# Codex

在网页新建会话时选择 **Codex**，填写宿主上的项目目录并选供应商。宿主通过 `codex app-server` 的结构化协议处理对话、工具调用、审批与提问。选择「宿主原生配置」时可以使用宿主已有的 ChatGPT 登录。

## 原生终端

```sh
ccr codex --new
ccr codex --new --supplier <供应商ID>
ccr codex --prompt "检查构建失败"
ccr codex resume --last
ccr session list --agent codex
ccr session attach <CCR会话ID>
```

终端由宿主持有，`Ctrl+]` 脱离后可以用同一个 CCR 会话 ID 接回。Codex TUI 使用宿主原生配置处理工作目录信任与审批。宿主重启或 TUI 退出后，用 `ccr codex resume` 选择已保存的历史。一个项目已有 Codex 终端时，先按提示接入现有终端，避免并行恢复可能正在运行的同一段历史。默认的宿主更新会等待 Codex TUI 退出，再重启服务。

新建终端的供应商来自与网页共用的目录，包含本地 API 配置、可用的 CC-Switch 配置和「宿主原生配置」。用 `ccr profile ls --app codex` 查看 ID，用 `--supplier @native` 指定宿主原生配置。

`ccr codex resume` 和 `ccr --resume <原生ID>` 都沿用会话记录中的供应商。尝试指定不同的供应商，或原供应商已不可用时，宿主会报错。对于尚未登记到 CCR、也没有供应商记录的原生历史，首次恢复默认使用宿主配置，也可通过 `--supplier <供应商ID>` 显式指定。新会话默认供应商只影响新建。

从一个供应商的终端切换到另一个供应商的历史时，退出当前 TUI 后用 CCR 的恢复命令。TUI 内的 `/resume` 由 Codex 按当前进程配置处理。

使用宿主原生配置时可传入 Codex 的 `--profile`、`--oss` 和 `--local-provider`，例如 `ccr codex --new -- --profile <名称>` 或 `ccr codex resume <原生ID> -- --profile <名称>`。

## 准备

安装 Codex CLI。使用 ChatGPT 登录时，在运行宿主的同一个系统账户下执行 `codex login`。宿主默认从 PATH 找到 `codex`，配置与历史目录为 `CODEX_HOME` 或 `~/.codex`。指定自定义目录时，先创建目录，再按需登录。

可直接指定路径：

```sh
cc-remote --codex /path/to/codex --codex-home /path/to/codex-home
```

后台服务使用相同配置：

```sh
cc-remote service config --codex /path/to/codex --codex-home /path/to/codex-home
```

Windows 上支持 npm 安装生成的 `codex.cmd`；宿主直接调用其 Node 入口，不经过 shell 拼接参数。服务启动后找不到 CLI 时，用 `--codex` 指定完整路径。

## 会话与设置

- 一个活跃托管会话对应一个常驻 app-server。消息按发送顺序执行；停止只中断当前轮，排队消息继续处理。
- 线程 ID 在创建后保存，空闲释放进程或重启宿主后，下次消息通过 `thread/resume` 继续。`/clear` 开始新线程，保留网页历史。
- 模型列表来自本机 Codex 的 `model/list`。可填写自定义模型 ID；模型与推理强度是否可用由当前 Codex 和账户决定。
- 新建会话用上下两块卡片选择 Claude / Codex；两种提供方共享最近项目目录。权限、模型和推理强度可在创建时配置，Full Access 对应 `danger-full-access`。
- 运行时顶部显示 CLI 返回的模型、推理强度、权限和上下文占用。点击配置摘要可切换模型、推理强度；修改在下一轮生效，当前任务继续运行。权限和供应商修改仍等待进程可安全重启后生效。
- 在「设置」中切换到 Codex 可修改独立默认值，保存在数据目录的 `codex/preferences.json`。Claude 默认值、模型别名和权限模式不会用于 Codex。
- 支持 `/model`、`/effort`、`/permission-mode`、`/status`、`/config`、`/compact`、`/clear`、`/stop`；在消息里使用 `$技能名` 调用 Codex 自己的技能。
- 命令执行和文件修改审批使用网页现有卡片；允许仅作用于本次请求。Codex 的用户提问也可在网页回答。
- 执行中的普通发送仍进入队列。宿主声明支持 `codexSteer` 后，网页另提供「追加到当前任务」：仅文本，没有排队消息或待处理审批时可用；当前轮次结束后会拒绝，不会自动开启新任务。旧版 Codex 不支持该 RPC 时会明确报错。
- 追加操作要求发送 ID，宿主保留最多 512 条、一小时内的去重记录；若连接异常导致送达不确定，保留提示并停止该连接，不自动重发。「确认追加结果」使用只读查询；记录过期、被淘汰或宿主重启后返回未知，不会重新发送，应先检查会话。
- `request_permissions` 的扩展权限请求可在网页查看并批准或拒绝。批准只回传本次请求的范围，固定为当前轮次有效，不授予整个会话的权限。未知权限结构会安全拒绝。
- 此审批入口取决于 CLI 是否开放该工具。本机 Codex CLI 0.164.0 的 `request_permissions_tool` 为开发中功能，默认关闭；宿主不会自动修改用户配置开启它。真实联调在隔离配置中显式开启，并通过本轮目录写权限请求验证。
- 执行计划和本轮累计代码差异以可折叠卡片展示；同轮更新替换旧卡片。会话用量读取最新累计快照，不重复相加，缺失信息不当作零；不推算费用或订阅额度。
- 「终端历史」选择 Codex 后，可查看本地 rollout 记录并导入为托管会话继续；导入前先停止原终端中的该线程。

| 沙箱模式 | 行为 |
| --- | --- |
| 跟随默认 | 沿用 Codex 配置的沙箱；审批交给用户 |
| `read-only` | 只读沙箱，额外权限通过审批请求 |
| `workspace-write` | 可写工作区，额外权限通过审批请求 |
| `danger-full-access` | 无沙箱并跳过审批；保存前需确认 |

## 订阅用量

订阅用量移到会话顶部，随当前提供方切换。Codex 使用 CLI 的 `account/read` 和 `account/rateLimits/read` 读取官方订阅额度，展示实际窗口时长、已用比例和重置时间；不启动推理，也不从会话 token 数估算额度。服务状态详情中可分别查看 Claude 和 Codex。

API Key、第三方供应商和未登录状态会明确说明原因；读取失败时标记缓存过期。额度查询按提供方和会话隔离，供应商会话不会显示官方账号的额度。

## 当前边界

Codex 支持托管会话、历史导入和原生终端接入。终端里的原生命令由 Codex TUI 处理。官方订阅用量以 Codex 返回的额度窗口展示。

原生 review/fork/goal 工作流、MCP elicitation/OAuth，以及会话级或策略级授权尚未映射到网页。普通 MCP 工具仍由 Codex 按配置执行。新增界面读取能力标志和可选事件，连接旧宿主时不调用新增接口。

协议依据：[OpenAI Codex App Server](https://developers.openai.com/codex/app-server/)。适配器测试使用模拟 JSON-RPC 子进程和合成历史，不依赖模型额度。本机 Codex CLI 0.164.0 已通过真实模型目录发现；完整结果与尚未验证的范围见 [verification.md](verification.md)。


## 发送栏、待发送区与任务活动

发送栏采用单行操作：附件图标、命令、权限、上下文圆环和发送按钮；窄屏下权限收为图标。发送按钮旁的下拉配置发送快捷键，模型和思考强度在顶栏状态详情中切换。顶栏额度为账号共享的 5h / 7d（或服务实际返回的窗口），显示已用百分比与重置倒计时。上下文圆环单独显示当前会话占用，不把累计 Token 当作上下文。

新宿主通过 `queueControls` 能力提供待发送区。默认等待当前任务结束，逐条可原位修改文本、保留附件、移除或立即投递。已经交给 CLI 的消息不能再编辑。Codex 的立即投递使用当前轮次的 steer，仅支持普通文本；附件等待下一轮。送达状态不明的指令单独提示，不阻塞后续队列，也不会自动重发。进程退出时保留未执行正文和附件并标注状态。

Codex 会展示协议实际报告的公开思考摘要、命令/文件操作输出和 MCP 工具进度。增量按轮次及工具项聚合，详情可折叠；计划显示已完成步骤数，不估计整体完成百分比。模型未报告的摘要保持缺省。
