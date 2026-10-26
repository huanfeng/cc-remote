# Codex

在网页新建会话时选择 **Codex**，填写本机项目目录即可。宿主调用本机已登录的 Codex CLI，通过 `codex app-server` 的结构化协议处理对话、工具调用、审批与提问；不需要单独配置 API Key。

## 准备

安装 Codex CLI 并在运行宿主的同一个系统账户下执行 `codex login`。宿主默认从 PATH 找到 `codex`，配置与历史目录为 `CODEX_HOME` 或 `~/.codex`。指定自定义目录时，先创建目录并在该配置下登录。

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

## 当前边界

Codex 目前提供托管会话和历史导入；`ccr`、本地终端接力和网页终端仍用于 Claude Code。Codex 原生命令不会当作 Claude 命令或 Claude 技能执行。订阅额度面板仍是 Claude 账户的额度；不把 Codex 使用量推算为美元费用。

原生 review/fork/goal 工作流、MCP elicitation/OAuth，以及会话级或策略级授权尚未映射到网页。普通 MCP 工具仍由 Codex 按配置执行。新增界面读取能力标志和可选事件，连接旧宿主时不调用新增接口。

协议依据：[OpenAI Codex App Server](https://developers.openai.com/codex/app-server/)。适配器测试使用模拟 JSON-RPC 子进程和合成历史，不依赖模型额度。本机 Codex CLI 0.164.0 已通过真实模型目录发现；完整结果与尚未验证的范围见 [verification.md](verification.md)。
