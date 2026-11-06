# 旧版前端功能清单与协议（web/ → web-next/ 迁移验收依据）

源码：`web/src`（约 6600 行，`App.tsx` 是中枢）。后端路由：`internal/server/server.go` `Handler()`。

## 1. 功能区

- **A 启动/登录**
  - 拒绝在 iframe 中运行
  - visualViewport 变化写入 `--app-height`，避免软键盘遮挡
  - 注册 sw.js
  - 令牌登录页：显示宿主身份，含语言切换
  - 安全须知勾选（`cc-safety-ack:v1`）
  - 明文 http 提示条
  - 登出

- **B 多宿主/独立版**（hosts.ts、HostSwitcher、HostLook）
  - 独立版首页：无宿主时为引导，有宿主时连接并每 10 秒重试
  - 添加/编辑宿主
    - 连接方式：direct，或 text 入口 URL
    - 地址、令牌、LAN https 地址、别名、颜色、图标
    - 校验 `/api/info`
    - 显示 `--allow-origin` 提示
  - 宿主切换弹层
    - 逐个 ping 显示离线/等待/运行数
    - Ctrl+点击或长按在新标签页打开（`?host=`）
  - LAN 自动切换：探测 LAN 地址，失败回退外网并封锁 1 分钟
  - 地址迁移：只自动跟随端口变化
  - 宿主身份：标题、favicon、`--host-color`，标题前缀 `(n)` 显示等待数

- **C 侧栏**
  - 品牌区：provider 可用点、LAN 标记、版本
  - 新建按钮
  - 标签页：会话 / 终端历史（Claude/Codex 下拉切换）
  - 搜索标题与 cwd
  - 缓存计数
  - 会话行：状态点、标题、相对时间（估算时加 ≈）、cwd、provider·模式·状态
    - 先渲染 200 行，其余点"显示更多"
  - 行菜单：释放缓存 / 退出会话（需 queueControls）/ 删除
  - 底部导航：设置、宿主状态
  - 桌面端：可折叠、可拖宽 200–480 且不超过 40%、双击复位、支持键盘调整
  - 移动端：抽屉
  - 列表每 3 秒轮询，并缓存到 localStorage

- **D 路由与标签页**
  - 路由：`#/s/<id>`、`#/native/<id>`、`#/native-codex/<id>`
  - 多个打开的标签页，各自保存事件、请求、草稿、附件、滚动位置、展开状态、问题草稿
  - 恢复：localStorage `cc-ui:v1`，sessionStorage `cc-ui-view:v1`
  - 草稿保留 7 天，总量约 1MB，跨标签页同步

- **E 顶栏**
  - 标题、状态徽章（打开 SessionStatus 弹层）
  - 芯片：供应商、provider、模型、effort（QuickSwitch）
  - cwd
  - 用量条：桌面为 5h/7d，手机为圆环
  - 对话 ↔ 终端视图切换（`cc-term-view`）
  - 停止
  - 任务面板（显示运行数）
  - 刷新、会话详情、⋯ 菜单
  - SessionStatus 弹层：已上报与已配置的模型/effort/权限、下一轮生效的设置、队列、后台任务、上下文、tokens、"从本地终端接入"
  - QuickSwitch
    - 模型来自 `/api/models` 按来源分组，别名和最近使用为子菜单
    - effort 选项按 provider 或模型
    - 终端会话：确认后发送 `/model X`（提示缓存会失效）
    - 托管会话：PATCH

- **F 新建会话**
  - provider 卡片
  - 供应商选择（Default / @native / CC-Switch 档案）
  - Claude：终端或托管（`cc-new-mode`）
  - 绝对路径，最近项目芯片最多 8 个
  - 名称
  - 终端模式需勾选"信任此文件夹"（否则返回 428 trust-required）
  - 权限、effort、模型覆盖：危险模式需二次确认

- **G 原生历史**
  - 只读记录
  - 以终端会话继续（Claude）
  - 导入为托管会话：确认已停止，可选供应商

- **H 对话流**
  - 加载：首屏 `tail=200&compact=1`，之后每 1.5 秒 `events?after=` 加 `/api/requests`
  - 可见标签页附加 `viewing=1`
  - 409 → 重置并重新加载
  - 向上翻页：IntersectionObserver 加载 `before=&limit=200`，保持滚动位置
  - 截断的工具调用打开时取 `/events/{seq}`；问题/计划卡片立即取全文
  - 折叠规则：
    - delta 合并为一条
    - 子代理事件按 `parent_tool_use_id` 嵌套
    - 隐藏 task-notification
    - 拆出 teammate
    - recap 被后续替换
    - Codex plan/diff 按 turnId 替换
    - activity 按 item 合并
  - 渲染：
    - 用户/助手 Markdown：GFM、换行、CJK 自动链接、文件路径链接、带令牌加载图片、代码高亮+复制、★ Insight 样式、锚点
    - 斜杠命令芯片
    - 附件、error
    - compact 分隔线（显示 pre→post tokens）
    - recap（可关闭，`cc-hidden-recaps`）
    - teammate
    - ToolGroup：摘要、失败/运行徽章、耗时、行详情（60 行/6000 字后"显示全部"）、打开文件
    - AgentCard：类型、前台/后台、状态、进度、元信息、嵌套记录
    - QuestionCard
    - Codex 卡片：plan、diff（最多 2000 行）、activity
    - Codex token 统计
  - 底部状态："X 正在工作…"/压缩中；后台任务数
  - TurnRail
  - 回到底部按钮（离底 >240 显示、<40 隐藏）
  - 远离底部时输入框缩成单行

- **I 审批/提问/计划**
  - RequestCard：允许 {tool}？/ 问题 / 确认计划 / Codex 追加权限
    - 按钮：在终端回答、拒绝、聊聊这个、提交/允许一次
    - 发送 `POST /api/requests/{id}`
  - 终端会话的共享提问：网页与终端竞争，先答者生效
    - AskCard：单选/多选、选项预览、自定义答案
    - PlanCard：Markdown 超 16 行折叠、批准后模式、反馈、继续规划/批准
    - 草稿按请求 id 保存
    - 结果来自 interaction 事件（web/handoff/terminal/expired）
  - PermissionPicker：危险模式先确认，`confirmDangerous`

- **J 输入框**
  - 自动增高，超出时显示放大按钮；IME 安全
  - 发送键：Enter 或 Ctrl+Enter（`cc-send-key`）
  - 运行中占位提示：消息会在当前任务后排队
  - 附件：+ 按钮、粘贴、拖拽；最多 8 个、单个 20MB；`POST /uploads`；图片缩略图+灯箱
  - @ 提及（mentions）：防抖 120ms，选中目录后菜单保持打开
  - 斜杠命令
    - 菜单来自 `/commands`
    - `/model`、`/effort` 打开 QuickSwitch
    - `POST /commands` 后按 `{message, openSettings, sendMessage}` 处理
  - 建议栏（终端会话空闲且草稿为空）：发送或编辑
  - 行内权限选择器
  - 上下文圆环
  - Steer（Codex + codexSteer）：仅在运行中、无队列、无请求、非命令、无附件时可用；回复丢失时改为"检查"
  - 待发队列（queueControls）：立即发送、编辑、删除，显示投递状态
  - Esc 停止

- **K 终端**
  - xterm：fit、unicode11、web-links、文件链接；回滚 3000 行
  - 连接：`POST /api/terminals/{id}/ticket` 后建 WS
  - 状态
    - loading / connecting / open / exited（4001，"再次启动"）
    - paused（页面隐藏时断开）
    - failed（12 次指数退避，打开超时 15 秒）
  - 尺寸
    - 桌面默认控制尺寸，被他端调整后转为跟随，"用此窗口"夺回
    - 手机默认跟随，"适配设备"，A−/A+ 缩放 0.5–3
    - held 时始终跟随
  - 重绘
  - 手机端
    - 按键栏：Esc、Tab、⇧Tab、方向键（遵循应用光标模式）、^C、Enter
    - 输入框：bracketed paste，300ms 后补 `\r`
  - Ctrl+C 有选区时复制
  - 接入弹窗：`attachCommand + id[0:8]`

- **L 文件查看器**
  - 路径识别：消息、代码、终端中的路径都可点击
  - 弹窗带返回栈
  - 图片：缩放、捏合；SVG 用 data: URL
  - Markdown：渲染与源码切换，>2MB 显示纯文本
  - 代码：高亮并跳转到 `:line`
  - 二进制与 PDF 提供下载
  - 413 时经 `POST /file/link` 下载
  - 复制路径

- **M 用量/状态**
  - useUsage 每 60 秒轮询，失败时标记 stale
  - StatusPanel 每 5 秒刷新
    - 宿主信息卡、会话计数、累计统计
    - 用量：provider/账号标签、历史图表（24h/7d/30d）、14 天图表
    - 会话表、日志尾部
  - SessionSettings
    - 标识、执行状态、供应商切换
    - 配置：已上报与已配置、来源
    - 覆盖编辑：恢复继承，有未保存保护
    - 统计、删除
  - DeleteSessionDialog：运行中需带 `stop=1`
  - TaskPanel：点击定位到对话中的调用

- **N 设置**（四个分区，有未保存保护）
  - 1. 会话默认：provider、默认供应商、CC Remote 默认值、原生 settings.json（高级）
  - 2. OAuth 令牌：增、替换、删
  - 3. 本宿主：版本、状态、身份（名称/颜色/图标）
  - 4. 本浏览器：语言（切换后重载）、主题、发送键、通知

- **O i18n**
  - 字符串写成 `t(en, zh)` 双语对
  - 语言在页面生命周期内固定
  - 请求头 `X-CC-Lang`

- **P 通知**
  - waiting/done/failed，由列表轮询的变化计算；首轮不提示
  - 焦点会话不通知
  - SW 通知，点击后路由到会话；偏好存 `cc-notify`
  - 标题徽章

- **Q 移动端**
  - 抽屉、徽章、用量圆环
  - 权限选择器缩成图标
  - 菜单收纳更多操作
  - 终端增强
  - 视口跟踪
  - 长按
  - 紧凑输入框
  - 灯箱

## 2. 协议

**通用约定**
- 请求头：`Authorization: Bearer`、`X-CC-Lang`、`cache: no-store`
- 不带游标的 GET 使用 ETag/304
- 错误格式 `{error}`；401 时清除令牌
- 请求体上限 2MB，后端 `DisallowUnknownFields`
- CORS 只放行 `--allow-origin` 中的来源
- 对话没有 WebSocket，全靠轮询：列表 3 秒、标签页 1.5 秒、状态 5 秒、用量 60 秒

**端点**

| 端点 | 要点 |
|---|---|
| GET `/api/info` | `{version,cwd,claudeAvailable,claudeVersion,codexAvailable,codexVersion,providers,attachCommand,restartPending,language,name,color,icon,features[]}` |
| GET/POST `/api/sessions` | 创建：`{provider,profileId,title,cwd,mode,trustFolder,resume,claudeArgs,permissionMode,model,effort,confirmDangerous}` → 201；428 trust-required |
| GET `/api/sessions/{id}?tail&compact&viewing` | `{session,events,hasMore}` |
| `/events?after=` | 返回 `Event[]` |
| `/events?before&limit` | 返回 `{events,hasMore}` |
| `/events/{seq}` | 返回完整事件（不截断） |
| PATCH/DELETE `/api/sessions/{id}` | DELETE 加 `?stop=1` |
| POST `/messages` | `{text,attachmentIds,defer,sendId}` → 202 `{status,duplicate}` |
| GET/POST `/commands` | — |
| `/stop`、`/exit` | — |
| `/steer` | 失败 409 带 `delivery`；查询用 `GET /steer/{sendId}` |
| `/queue/{mid}` | PATCH 或 DELETE；立即发送用 `/send` |
| `/uploads` | multipart；读取用 `GET /uploads/{uid}` |
| `/file?path&base` | 带 X-CC-Path；过大 413；下载链接 `/file/link` → `/api/download/{ticket}` |
| `/mentions?q` | 返回 `{paths}` |
| `/tasks`、`/stats`、`POST /profile {profileId,force}` | — |
| GET `/api/requests?sessionId`、POST `/api/requests/{id}` | POST：`{decision,answers,mode,message}` |
| `/api/native/sessions[?provider=codex]` | 同托管会话的分页接口；导入 `POST /{id}/import {confirmStopped,profileId}` |
| `/api/preferences[?provider]` | 带 revision |
| `/api/claude/settings` | — |
| `/api/models?sessionId&refresh&provider` | — |
| `/api/identity` | — |
| `/api/profiles` | 默认值 `/default`；OAuth 增删改 `/oauth` |
| `/api/usage?sessionId\|profile&provider` | 历史 `/api/usage/history?hours` |
| `/api/status/summary`、`/api/status` | — |

**终端 WS**
- 地址：`/api/terminals/{id}/stream?ticket=`
- 客户端 → 服务端
  - 二进制帧：按键
  - `{type:'resize',cols,rows}`
  - `{type:'redraw'}`
- 服务端 → 客户端
  - 二进制帧：PTY 输出
  - `{type:'size',cols,rows,held}`
- 关闭码：4001 = 已退出（不再重连），其余关闭都重连
- 服务端每 30 秒 ping

## 3. 类型（详见旧版各文件）
- 会话与事件：`Session`、`Event`（kind 包括 user/assistant/assistant_delta/tool/tool_result/error/compact/recap/teammate/status/result/session/interaction）、`Request`、`Info`
- 实时状态与队列：`LiveStatus`、`PendingMessage`、`Attachment`
- 工具与任务：`ToolCall`、`TaskInfo`、`QuestionDraft`
- 宿主与供应商：`HostEntry`、`SupplierProfile`、`ProfileCatalog`
- 用量与状态：`UsageReply`、`Stats`、`HostStatus`、`CodexStatus`、`ModelList`
- Go 源头：`internal/store/store.go`、`internal/server/tasks.go`、`internal/server/usage.go`

## 4. 易碎点
1. Esc 停止依赖 DOM 类名判断有无弹层 → 新版改用浮层栈。
2. sendId：每个标签页（及 suggestion、steer）保持同一 id，直到发送成功；steer 回复丢失时改为查询。
3. 宿主能力门控（DisallowUnknownFields）。
4. ETag 返回同一对象时跳过渲染；`stabilize` 复用 ToolCall 对象。
5. 过期异步回复保护（navigationVersion）；列表轮询有在途请求时跳过。
6. compact 模式下超过 2000 字节截断。
7. 共享提问的匹配：工具名 + questionKey；ExitPlanMode 只按工具名匹配。
8. 终端尺寸：最后调整者胜出；5 秒内收到的回显算自己发出的；CSP 拦截不触发 close 事件，所以设打开超时。
9. 终端会话切换模型实际是键入 `/model`，需先确认。
10. `confirmDangerous`。
11. 后台任务比轮次活得久；done 通知要等 activeTasks 归零。
12. 独立版安全：iframe、SVG 用 data: URL、入口地址只自动跟随端口变化。
13. storage 可能抛异常。
14. 终端会话的斜杠命令（除少数几个外）实际是键入 PTY。
