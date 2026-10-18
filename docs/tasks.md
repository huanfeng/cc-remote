# CC Remote 任务清单与续接记录

更新日期：2026-10-15。技术栈：Go 宿主、React/TypeScript PWA、SQLite；优先 Claude Code，Windows/Linux 宿主。不做网络穿透，不以 TTY 镜像作为网页交互。

## 已完成的基础版本

- [x] 托管多会话：结构化对话、工具过程、权限审批、提问、恢复上下文、停止执行。
- [x] 原生历史查看和导入，直接沿用原会话标题，不要求命名。
- [x] 原生终端与网页通过 Claude Channels 共用一个交互式 Claude 进程；消息、工具审批和历史同步。（共享模式已移除，由终端会话取代；已有共享会话在宿主启动时迁移为终端会话。）
- [x] 共享终端（现为终端会话）AskUserQuestion / ExitPlanMode 双端作答：`PermissionRequest` Hook `channel-ask-hook` 长轮询 `POST /api/channels/{id}/interactions`，网页经 `POST /api/requests/{id}` 回答（`decision: allow|deny|terminal`，计划可带 `mode`/`message`），终端先答时 Hook 轮询 transcript 退出、宿主同步兜底；`interaction` 事件记录结果（web/handoff/terminal/expired），网页卡片原位作答并显示「已在终端/网页回答」、「交给终端」。真实 Claude Code 2.1.300 + haiku 验证网页回答、终端回答、网页批准计划（acceptEdits）三种路径。
- [x] 连接代际隔离、离线拒绝、队列确认、审批一次性响应、会话 ID 变更断开保护。

## 本次迭代

- [x] 空白页新建/连接按钮、连接说明复制按钮增加内边距及间距，手机纵向排列；复制支持普通 HTTP 降级、成功反馈及手动选择兜底。
- [x] PC 与手机统一使用左侧会话列表（手机为抽屉菜单），取消顶部标签和重复的已打开列表。打开过的会话显示「已缓存」，切换保留消息、草稿、截图、滚动位置、工具展开和待提交回答。
- [x] 侧栏显示执行中、等待操作、已完成、失败状态；执行中有动态圆点。后台缓存会话持续刷新，即使正在查看终端历史。
- [x] 「×」释放指定会话的页面缓存，清除该会话未发送草稿；宿主任务继续运行，已发送历史不删除。
- [x] 紧凑对话布局、可折叠左栏，手机使用抽屉菜单。
- [x] Enter / Ctrl+Enter 发送可选并记忆；Shift+Enter 换行，中文输入法组合输入不发送。
- [x] 登录令牌保存在本站点 localStorage；退出及凭证失效时清除。
- [x] 上传/粘贴 PNG、JPEG、GIF 截图、预览及消息回看；每张 10 MiB、2500 万像素，每条最多 8 张，Claude 通过原生 Read 读取。
- [x] Claude 用户级模型、推理强度、权限默认设置；保存保留其它字段并检测版本冲突。
- [x] 网页内置 `/help`、`/status`、`/model`、`/effort`、`/config`、`/permissions`；托管模式支持 `/clear`、`/stop`。
- [x] 权限/模型/推理强度改为 CC Remote 自管：会话覆盖 → `preferences.json` 默认 → Claude 配置，以启动参数传入，支持 `bypassPermissions` 等全部模式（危险模式需确认）；共享终端启动器自动追加默认值。动态模型列表 `GET /api/models`（别名/配置/最近使用/API）与共用模型选择器；可选写入 `~/.claude/settings.json` 改为原位编辑、保留格式并备份。
- [x] 发现用户/项目 `skills/*/SKILL.md` 和旧式 `commands/**/*.md`，支持静态 `$ARGUMENTS` 展开，原始指令保留在聊天记录。
- [x] `cc-remote -- claude [原生参数]`：自动启动本机宿主，终端保留原生体验；旧 `terminal` 命令兼容。
- [x] 用户数据目录：`--data-dir` 优先，其次 `CC_REMOTE_DATA_DIR`，默认 Windows `%APPDATA%\cc-remote`、Linux `${XDG_CONFIG_HOME:-~/.config}/cc-remote`。

## 服务化迭代（2026-10-15）

- [x] `cc-remote service install|config|start|stop|restart|status|logs|uninstall`：Windows 登录计划任务 + GUI 子系统监督进程（无窗口、Job Object、崩溃退避重启），Linux `systemd --user`，macOS LaunchAgent。重装/更新沿用原配置。
- [x] 多地址监听：可重复 `--listen`、`lan:PORT`、`localhost:PORT`；始终保留回环地址；`all:`/`0.0.0.0` 需 `--allow-insecure-remote`；非回环地址启动告警。
- [x] 服务环境变量：安装时自动捕获代理变量（`--env`/`--capture-env`），解决服务环境不加载 shell profile 导致的 `403 Request not allowed`。
- [x] `ccr` 别名（`ccr.cmd` / `~/.local/bin/ccr`，加入用户 PATH）；继承网页危险权限模式时终端告警。
- [x] `/api/status`、单会话统计、`/api/admin/shutdown`（仅本机）；网页「服务状态」面板（统计、14 天图表、日志尾部）。
- [x] 日志文件 `--log-file`（10 MiB 轮转、崩溃输出），`cc-remote version`。
- [x] 发布：`ci.yml`（测试、govulncheck）、`release.yml`（6 平台包、checksums、构建来源证明）、`scripts/install.{ps1,sh}` 私有仓库一键安装/更新、`cc-remote update`；工具链 go1.26.9。

Windows 服务实测：计划任务无窗口运行、强杀后 5 秒内重启、优雅停止、`status` 退出码 0/3、重装沿用配置、bypassPermissions 托管轮次零审批执行 Bash。
注意：从 MSIX 打包应用（如部分桌面 AI 客户端）的终端启动宿主时，`%APPDATA%` 写入会被虚拟化到 `%LOCALAPPDATA%\Packages\<应用>\LocalCache\Roaming`，与服务看到的真实 `%APPDATA%\cc-remote` 不是同一目录；迁移时需从虚拟化目录复制数据，并从普通终端运行 `ccr`。
已在 GitHub Actions 实跑 CI 与 Release（预发布 `v0.2.0-rc.*`），并对真实 Release 验证安装脚本 dry-run、下载与 sha256 校验；未验证：实际安装/自更新替换、Linux/macOS 实机服务。

### 界面调整（2026-10-15）

- [x] 顶栏只放当前会话：标题、目录、模型 · 推理强度 · 权限模式只读标签（继承值「默认(…)」，危险模式着色）、状态、刷新、「会话设置」；移除常驻会话控制条。
- [x] 会话设置对话框：模型/推理强度/权限模式（保存、取消、全部恢复默认，危险模式二次确认），会话 ID、Claude 会话 ID、目录、模式、创建时间与用量摘要；共享终端只读；危险区删除入口。
- [x] 全局「默认设置」「服务状态」移到侧栏底部导航（手机抽屉同）。
- [x] 删除会话：`DELETE /api/sessions/{id}[?stop=1]`，单事务删除 events/interactions/channel_transcript/会话行，清理内存待审批与共享连接状态、`uploads/<id>/`（`filepath.Rel` 校验不越界）、状态缓存；执行中 409（`stop=1` 先停止，最多等 15 秒）、在线共享终端 409；不动原生 JSONL。侧栏删除图标 + 自定义确认框。
- [x] 任意文件附件：单个 ≤ 20 MiB、每条 ≤ 8 个，图片保留解码/像素校验，其它文件以清理后的原名保存并记录 MIME；下载走带令牌的 GET，非图片 `attachment` + nosniff，HTML/SVG 等以 octet-stream 返回；粘贴、拖放、文件卡片。
- [x] 统一细滚动条（WebKit 样式 + Firefox `scrollbar-width`），对话框限高 `100dvh - 32px` 内部滚动且滚动条不压圆角。
- [x] 桌面侧栏拖动调宽（200–480px、≤ 视口 40%、双击复位、键盘可调、本机记忆，折叠兼容）。

## 使用约定与边界

- PC/手机切换会话不会丢界面缓存；刷新页面按 URL hash 回到原会话并恢复打开列表、草稿、附件和未提交回答（localStorage，7 天过期），滚动与展开状态按标签页恢复（sessionStorage）；释放缓存会清除该会话草稿，已发送消息保存在 SQLite。
- 网页设置修改用户级 Claude 默认值；项目/组织设置可能覆盖。托管下一轮读取，运行中的终端会话需重启 Claude Code 或在终端用原生命令即时修改。
- 托管会话中 Claude init 事件 `slash_commands` 列出的命令（内置、插件、技能）以 `-p --resume` 原样交给 Claude（2.1.300 实测 `/compact`、`/context`、`/usage` 可用；`/clear` 在 `-p` 下会换会话 ID，故由 cc-remote 自己重置）。
- 终端会话的提问选项和计划确认依赖 `PermissionRequest` Hook 与原生对话框并行（2.1.300 实测），Hook 被禁用时回退为仅终端作答。
- 图片保存在宿主，取消附加不会删除已上传文件；尚未实现清理策略。
- 同项目多会话会操作相同文件，尚无自动 worktree 隔离。
- PWA 支持静态资源缓存，尚无离线消息队列或推送通知。

## 验证与交付状态

- [x] Go 全量竞态测试、`go vet ./...`。
- [x] 前端 TypeScript / Vite 构建。
- [x] Linux amd64、CGO 关闭交叉构建；尚未进行 Linux 实机联调。
- [x] Windows 真实 Claude Code 共享启动和网页截图发送，Claude 正确识别截图中的标题、按钮；网页完成 Read/reply 审批。
- [x] 浏览器验证多会话独立草稿、截图保留、折叠侧栏和设置读取。
- [x] 最终移动端菜单、快捷键和刷新免登录浏览器回归；最新侧栏版本在 PC 和手机视口验证缓存标记、切换、执行/等待/完成状态，顶部标签已取消。
- [x] 更新默认宿主可执行文件并重启，保留现有用户数据。

数据目录迁移：旧版项目内 `./data` 的 SQLite、WAL 和访问令牌在宿主停止后复制到用户默认数据目录，验证原有会话完整保留；原目录保留不删。

最新侧栏回归：独立 8792 测试宿主使用真实 HTTP/SQLite 生命周期与模拟 Runner，不调用模型或执行工具；验证 A 执行中、B 等待操作、B 完成、A 草稿保留，以及释放 A 缓存后任务继续。该测试宿主已停止，临时夹具保留在 `.test-data/sidebar-qa`。当时的预览截图已由当前界面截图取代：[桌面](images/desktop.png)、[手机](images/mobile.png)。

真实测试记录见 [verification.md](verification.md)，使用方式见 [usage.md](usage.md)，设计与限制见 [architecture.md](architecture.md)。后续执行者先检查这里未完成项，并更新验证结果，避免重做已经完成的功能。

## 后续候选事项（尚未承诺实现）

- [ ] Linux 实机和真实手机 PWA 联调。
- [ ] 上传图片生命周期、存储占用显示及清理。
- [x] 会话草稿/打开列表跨刷新恢复：hash 路由 `#/s/<id>`、`#/native/<id>`，前进/后退切换；`cc-ui:v1` 保存打开列表、侧栏标签、草稿/附件/待提交回答（7 天过期、约 1 MiB 上限），sessionStorage 保存滚动与展开；已删除会话回到欢迎页并提示。
- [ ] 更完整的原生技能执行语义与控制命令适配，先核实官方接口再实现。
- [ ] 历史分页与大对话性能、连接状态和错误反馈优化。
- [ ] 推送通知、Codex 适配器、项目 worktree 隔离。
