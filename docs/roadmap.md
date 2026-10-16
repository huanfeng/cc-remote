# 路线图：补全会话模式

目标：会话（托管）模式能完整覆盖日常使用，包括子 agent 的查看与处理、上下文压缩、新上下文等；共享模式维持现状。

## 依据（Claude Code 2.1.300 实测）

- `-p --input-format stream-json --output-format stream-json` 可作为常驻进程：同一进程内连续多条消息、`interrupt` 控制请求打断当前轮（进程保留，响应含 `still_queued`）、发送 `/compact`、`set_model` 控制请求即时切换模型，压缩与切换后上下文保持。
- 子 agent 内部的消息带 `parent_tool_use_id`（指向发起它的 Agent 工具调用）；另有 `task_started`、`task_progress`（描述、`last_tool_name`）、`task_updated`、`task_notification`（完成状态）、`background_tasks_changed` 系统事件。
- `-p` 模式的 init 事件 `slash_commands` 列出可执行命令（已用于命令透传）；`/clear` 在 `-p` 下会换会话 ID（`conversation_reset`）。

## 阶段 1：常驻进程运行时（已完成）

- 每个活跃托管会话一个常驻 Claude 进程，空闲超时退出，下次用 `--resume` 拉起；进程意外退出时自动恢复。
- 停止改为 `interrupt` 控制请求，不再结束进程。
- 模型、推理强度、权限模式变化后，空闲时下一条消息重启进程生效（`set_model`、`set_permission_mode` 实测可用，留作运行中即时切换的后续优化）。
- 运行中发送的消息直接交给 Claude 排队（Claude 合并排队消息为一轮），按回显逐条记入记录；多个客户端同时发送按顺序写入；界面显示等待状态。
- 审批、提问、计划确认等现有桥接保持不变。

## 阶段 2：子 agent 与后台任务（已完成）

- Agent 工具调用渲染为可展开卡片，内部按 `parent_tool_use_id` 嵌套子 agent 的消息与工具调用，运行中显示 `task_progress`。
- 后台任务面板：运行中与已完成的任务及状态，可跳转到对应卡片或命令。
- 后续：终端历史与共享会话读取 `subagents/` 下的子 agent 记录并嵌套显示；停止单个后台任务。

## 加载与多设备

- 已完成：会话从末尾分页加载、长工具输出按需加载、原生历史与会话列表缓存、ETag 与 gzip。
- 多设备：同一份前端既可内嵌也可独立部署（Cloudflare Pages、GitHub Pages、Vercel 等）；宿主增加 `allowed_origins` 与 CORS，前端增加设备列表与 `#/d/<设备>/s/<id>` 路由，`/api/info` 返回 API 版本供前端适配。前端在 HTTPS 下只能连接 HTTPS 的设备（浏览器混合内容与局域网访问限制），局域网设备需要证书、隧道或 Tailscale。
- 推送替代轮询：用 SSE 或流式接口替代 1.5 秒轮询。

## 阶段 3：日常功能

- TodoWrite 任务清单面板、上下文用量提示、每轮耗时与用量。
- 会话重命名与 Claude 同步。

## 阶段 4：与终端交接

- 已由终端会话取代（见 [design/terminal-sessions.md](design/terminal-sessions.md)）：宿主持有交互式 Claude Code，网页和 `cc-remote attach` 同时接入，切换不中断任务。
- 终端会话阶段 1–2 已完成：伪终端运行器、hook 驱动的状态、网页消息与提问、`cc-remote attach`。
- 后续：网页内终端（xterm）、工具审批经 hook 送到网页、宿主升级前提示忙碌会话、共享会话逐步下线。

## 暂不做

- 共享模式的伪终端中转：终端会话已经用伪终端实现，共享模式保持 Channels 方案，不再另做。
