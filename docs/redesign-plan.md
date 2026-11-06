# 前端重做计划（web-next）

状态：进行中（2026-11-06 立项）

## 已定决策
- 新前端放在 `web-next/`，与 `web/` 并行；功能迁移完成后，Go 二进制改为嵌入 `web-next` 的产物。
- 技术栈与 ZCode 一致：React 19 + Vite + Tailwind v4 + radix-ui + cmdk + lucide-react + motion + shiki + zustand + @tanstack/react-virtual + @xterm/xterm。
- 大量借用 ZCode（Apache-2.0）的组件和设计令牌；最后阶段再出几套配色样板供挑选，定下自己的配色。
- 深浅双主题，默认跟随系统。
- 移动端：宽度 ≤767px 时由 JS 切换为单列外壳（会话列表 ⇄ 对话两页，配合 history 导航）。
- 桌面端：Tauri 2。
- 参考仓库：ZCode 仓库 `zai-org/ZCode`（`packages/ui/src`）；Codex 桌面端闭源，只参考它的视觉。

## 架构要点
- **Backend 接口**：所有数据访问都经过 `Backend` 接口（REST 调用 + 会话轮询 + 终端流）。阶段 1 用 `MockBackend`，阶段 2 换成 `HttpBackend`，UI 层不改。
- **浮层栈**：取代旧版靠 DOM 类名判断 Esc 是否该停止任务的做法；有浮层打开时（栈非空），Esc 不会停止正在运行的任务。
- **宿主能力门控**：后端用 `DisallowUnknownFields` 拒收未知字段，请求体必须按 `info.features` 构造（`sendId`、`mentions`、`queueControls`、`codexSteer`、`codex`）。
- **平台注入**：`Platform` 接口区分 web 与 tauri（窗口控件、通知、多宿主存储）。

## 借用清单（ZCode → web-next）
- `styles.css` 第 145–847 行的令牌（重命名 `theme-zai-*`），以及滚动条、闪光动效等片段；`shadcn/tailwind.css` 的 data-open/closed 变体；`tw-animate-css`。
- `components/lib/utils.ts`：`cn`，需注册 `text-ui-*` 字号组。
- `components/ui/*` 中可直接拷贝的约 30 个基础组件；dialog、spinner、kbd 需要 i18n 桩。
- ai-elements：conversation、prompt-input*、confirmation、queue、tool、shimmer、plan、terminal。
- 工具调用：`ToolCallBlocks/ToolLayout`、`ToolSummaryRow`，以及 execute/edit/read/search/todo 渲染器作为模板。
- 终端：`terminalTheme`、`terminalLinks`、`terminalDataTransform`、`terminalComposedInputFallback`。
- 移动端：`bottom-sheet`，以及 `WebRemoteControlMobileShell` 的布局模式。
- 桌面端：`DesktopWindowControls` 的模式，拖拽区域改用 `data-tauri-drag-region`。
- **禁止拷贝**：ZCode/Z.ai/GLM 标志，以及供应商、支付、渠道、插件图标和音效。
- **署名义务**：保留 Apache-2.0 许可证文本和 ZCode NOTICE、shadcn 的 MIT 声明、Vercel ai-elements 的逐文件来源头注释；修改过的文件需标注已修改。

## 阶段
0. 骨架：脚手架、令牌、基础组件、主题、i18n、NOTICE。
1. 模拟前端（MockBackend）：
   - 应用外壳：侧栏、会话列表、顶栏、多宿主切换
   - 对话流：消息、工具组、Agent 卡片、Codex 状态卡、压缩分隔线、recap
   - 输入框：附件、@ 提及、斜杠命令、Steer、待发队列、上下文圆环
   - 审批、提问、计划卡片
   - 终端模式：xterm 回放模拟输出，尺寸跟随/控制，手机按键栏
   - 文件查看器、新建会话、原生历史与导入
   - 用量与状态面板、会话详情、设置（四个分区）
   - 移动端外壳、通知
1.5 高保真打磨（对照 Codex/ZCode 截图）。
2. 用 HttpBackend 迁移真实功能，按旧版功能清单逐项验收。
3. 替换嵌入产物（web/embed.go），处理独立构建（standalone）和 CSP。
4. Tauri 2 桌面端。
5. 配色样板，由用户挑选。

## 进度
- 2026-11-07 阶段 0、1 完成：web-next 跑在 MockBackend 上；tsc 零错误，114 项测试通过；
  独立审查发现的 1 个 HIGH、10 个 MEDIUM 已修复。等待用户在浏览器中验收。
- 已定：新建终端会话时，「信任此文件夹」由宿主判断（未信任返回 428 后再提示勾选），不再每次强制勾选。

## 阶段 2 待办（来自阶段 1）
- HttpBackend：实现 `Backend` 接口（包括 BackendError、`fileLink`、`steerStatus`、`claudeSettings`、
  `replaceOAuthProfile`）；在内部轮询，节奏为列表 3 秒、会话 1.5 秒、用量 60 秒，并处理 ETag 和 409 重置。
- 多宿主：把旧 `hosts.ts`（LAN 切换、地址迁移、每宿主独立的存储键）移植到 `lib/hosts.ts`；启用「添加宿主」。
- 包体积：主包 gzip 后约 517 KB。对话框、终端和 shiki 语言包改为懒加载。
- 上传进度：`upload` 需要回报进度；Markdown 中的相对图片改为经宿主带令牌加载。

## 旧版功能清单与协议
见 `docs/redesign-inventory.md`。
