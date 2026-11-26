# 发布、安装与更新

本文说明如何发布 cc-remote 版本，以及在 Windows / Linux / macOS 上安装、更新和卸载。公开仓库可匿名下载；GitHub 凭据只在仓库私有或遇到 API 速率限制时需要。

## 发布新版本

### v0.46.0 更新内容

- 网页新建 Codex 会话时可选择终端模式。供应商和账号按所选模式提供可用模型与默认配置。
- 修复手机上新建会话和其他弹层的滚动问题，长表单和选项列表可以滑动到底部。
- 「已完成」会话的已读状态由宿主保存。在网页、手机或桌面端看完一条回复后，连接同一宿主的其他设备会同步清除该会话的未读标记。
- 主动切换供应商前提示缓存可能失效，下一轮对话可能产生更高的输入费用。勾选「下次不再提示」后，偏好保存在宿主上，并可在供应商设置中恢复提示。
- 宿主状态和会话详情在缺少金额时显示「暂无费用数据」。近 14 天图表只展示有记录的费用，宿主明确记录的零费用显示为 `$0.00`。

更新宿主后即可跨设备同步已读状态和切换提示偏好。新版独立网页连接旧宿主时仍可进行常规会话操作。

### v0.45.0 更新内容

- 会话顶栏可以直接打开或关闭文件侧栏。在侧栏浏览服务器项目文件，或用 `Ctrl+P` / `⌘P` 按文件名和相对路径快速打开。搜索跳过有缓存标记的构建目录。
- 文件侧栏显示 Git 的已暂存、未暂存和未跟踪改动，可以查看对应的 diff。
- 从文件预览引用路径或选中的代码行，也可以从 diff 引用新增行和删除前的原始行。引用会插入当前对话草稿。
- Windows 桌面端的「关于与更新」按组件显示版本和更新日志。目标服务器可独立检查和更新 CC Remote Core、Claude Code 和 Codex，并查看更新结果。更新 Core 时桌面客户端保持打开。

使用新版桌面端连接旧版 Core 时，项目文件搜索和 Git 改动入口需要升级目标服务器上的 Core 后才会出现。

### v0.44.0 更新内容

- 会话图片和文件可从预览对话框移到多标签文件栏，查看内容时可以继续撰写回复。
- 悬停历史消息可复制文本、从该处创建分支，或编辑之前的用户消息并在新分支发送。分支支持 Claude Code 和 Codex，源会话保留原有记录。
- Claude Code 终端执行 `/compact` 时即时显示压缩状态。桌面宿主标签用蓝色数字显示正在运行的会话数。
- 桌面端的 xterm 链接使用系统浏览器打开，拦截 Tauri 空白弹窗。关闭宿主标签前，关闭按钮旁会显示确认气泡。
- 项目终端的分屏支持拖动分隔线调整大小。桌面端最多保留 10,000 行回滚，移动端最多保留 5,000 行，单次运行的宿主输出缓冲上限为 4 MiB 或 16,384 个事件。
- Windows 桌面端可在「关于与更新」更新已注册为服务的本机 Core，等待会话空闲后重启服务，并显示实际运行的版本。

### v0.43.1 修正内容

- 会话图片附件在当前会话内打开预览，支持缩放和拖动。
- 修复 Markdown 中的相对路径图片无法显示，以及桌面端按住 Ctrl 或 ⌘ 点击本地图片时意外打开窗口的问题。

### v0.43.0 更新内容

- 网页的 Codex 供应商设置支持添加多个 ChatGPT 账号。各账号独立保存登录状态、配置和会话历史。新建时可选择账号，恢复时沿用原会话所属账号。
- Codex 原生终端与网页共用供应商配置。CLI 恢复了 `ccr claude --new`、`ccr codex --new`、`ccr --resume <原生ID>` 和 `ccr -c` 快捷入口。
- 修复 Claude 长粘贴的包装标签出现在临时会话标题中的问题。超长标题现在以省略号显示，较窄的桌面顶栏会为操作按钮换行。

### v0.42.0 更新内容

- Web 顶栏悬停卡片和「宿主状态」详情沿用分段用量条。5 小时和每周额度分别按 5 格和 7 格显示距重置的剩余时间，保留文字倒计时，支持带重置时间的模型额度。
- Windows 托盘悬停面板使用同样的分段用量条，支持拖动边缘或角落调整宽度和高度，下次打开时恢复尺寸。
- 托盘右键菜单合并为一个「关于与更新」入口。后台检查发现新版时，托盘图标显示提示点，菜单标出可用版本，签名正式版继续发送系统通知。

### v0.41.0 更新内容

- CLI 统一 Claude Code 和 Codex 终端入口。用 `ccr claude` 或 `ccr codex` 打开终端，用对应的 `resume` 命令恢复原生历史，用 `ccr session attach <CCR会话ID>` 接回在线进程。
- 升级旧脚本时，将 `ccr --resume <原生ID>` 和 `ccr -c` 迁移为 `ccr claude resume <原生ID>` 和 `ccr claude resume --last`，供应商参数改用 `--supplier`。独立更新二进制后，可执行 `cc-remote alias install` 刷新旧启动器。详见[命令行参考](cli-design.md)。
- `ccr shell attach` 支持选择和筛选运行中的项目 Shell。项目 Shell 会拦截接入自身，防止终端输出循环回放。
- 会话顶栏显示当前项目的终端总数与在线状态。终端支持快捷键新建标签和分屏，浮动终端可自动收起，也可固定在会话上方。
- Web 顶栏的 5 小时和每周用量条分别显示 5 格和 7 格刷新倒计时。剩余时间从右侧按比例填充，悬停可查看具体时间。
- 网页输入框可用方向键查看已发送消息、返回草稿和接受下一步建议。提问、规划反馈、恢复会话和用量上限卡片支持按发送键偏好提交。
- 侧栏新增主题菜单，可快速切换应用主题、应用配色和终端配色。

### v0.40.0 更新内容

- 项目 Shell 和会话终端共用配色设置，默认深色，也可选浅色、深海蓝、暖棕或跟随应用主题。
- 项目终端按项目编号，默认名称为「终端 1」「终端 2」等。「运行中的终端」同时显示终端名称和所属项目。
- 项目 Shell 可在同一标签内反复分屏，支持混合左右与上下布局、循环切换窗格和调整分屏方向。
- 会话侧栏用紫色空心圆标识会话空闲但终端仍在线的状态。
- Web 供应商切换会隐藏明确未登录的 Claude 原生配置及指向它的默认选项。OAuth 的 5 小时和每周用量各自按用量高低显示绿、黄或红色。

### v0.39.0 更新内容

- 「运行中的终端」支持多选、全选和批量停止。部分停止失败时保留失败项的勾选，便于重试。
- 左右和上下分屏可从标签栏直接打开，两个窗格归属同一标签。标签支持中键关闭和右键原位重命名，搜索移到标签栏，标题栏右键可结束当前项目的全部终端。
- 退出或结束的终端从列表和恢复记录中移除。宿主重启后，重新接入时恢复仍打开的标签、分屏和启动目录，并启动新的 Shell 进程。升级前的历史记录缺少退出状态，如仍有旧入口，可用「结束所有终端」清理当前项目的记录。
- 项目终端可使用浏览器所在设备上的 Nerd Font 显示 Zellij 状态栏字形。也可以用 `zellij options --simplified-ui true` 启动简化界面。

### v0.38.0 更新内容

- 同一项目可打开多个独立 Shell，支持标签切换、重命名、左右或上下分屏。标签和终端区域支持右键菜单，搜索入口移到窗格工具栏。
- 项目终端支持搜索回滚输出、暂停跟随和跳到最新一行。「运行中的终端」可查看并接入活跃终端，`ccr shell ls` 显示名称和 ID。
- 结束最后一个终端时面板自动收起，再打开时启动新进程。宿主重启后，恢复成功的提示会自动消失。
- 新增已保存和原生会话的消息搜索。供应商设置集中管理 API 和 Claude OAuth 配置，并支持为 OAuth 账号添加备注。
- 优化宿主颜色和状态栏排序，以及 Windows 任务栏提醒的显示。中英文 README 和 About 页面补充项目 Shell、供应商切换与 Claude OAuth 的使用说明。

### v0.37.0 更新内容

- 项目 Shell 浮动面板支持拖动和调整大小，自动保存位置与尺寸，并可从终端菜单重置布局。
- 收起后继续接收输出，重新展开时保留清屏状态和滚动位置。桌面布局可回看最多 30,000 行，移动布局最多 15,000 行。
- 新版网页和宿主按批传输和解析终端输出，持续刷屏时仍可及时处理键盘输入和 `Ctrl+C`。断线后按上次解析的位置续接保留范围内的输出。

### v0.36.0 更新内容

- Claude 的 OAuth 与 API 供应商支持混合调整顺序，Codex 供应商可单独排序；新建会话、切换和默认配置选择共用顺序。排序保存在当前浏览器并按宿主区分，兼容旧版宿主。
- 订阅监控区分当前采集账号与历史登录记录，同名记录可区分；Codex 身份确认后不再留下临时的未知身份记录，已有账号历史继续保留。
- 桌面用量托盘默认只显示收藏，可切换到全部账号管理收藏；网页和托盘显示用量上次获取时间，并减少身份未确认造成的误导。

### v0.35.0 更新内容

- 网页标题和页签图标新增任务提醒，区分待处理事项与尚未查看的完成结果；桌面服务器标签页和 Windows 任务栏显示提醒数量，汇总当前窗口中所有已打开的服务器。
- 完成结果查看后自动清除提醒；已归档任务及仍有后台工作运行的任务不计入未读完成，首次加载历史记录不会产生大量提醒。提醒不依赖系统弹窗通知权限。
- OAuth 账号支持编辑名称，令牌留空保留原值；保存失败显示具体校验原因，默认配置和已有会话中的名称仍需手动更新。
- Web 供应商选择新增 OAuth 用量概览，显示缓存中的 5 小时 / 每周用量及数据状态，兼容缺少此接口的旧宿主。
- 登录过期会同步原始错误并更新失败状态；恢复会话的额度消耗提示可在网页选择恢复、新开对话或从摘要恢复，并阻止排队消息误触菜单。
- 终端选中文本后自动复制，右键保留并复制选区；复制失败时保留选区并提示重试。

### v0.34.0 更新内容

- 项目终端默认可用。可从会话标题栏或项目菜单打开，桌面应用默认按 `Alt+F` 开合，网页默认按 Ctrl 加反引号开合。
- 同一项目的 Web 会话与 CLI 共用 Shell。收起面板和断线时保留运行进程，重新连接后补齐缓冲范围内的输出。宿主重启会保留入口和启动目录。
- 新增 `ccr run`，在宿主执行一次命令并透传标准输入、标准输出、错误输出和退出码。
- 新增 `ccr shell`、`ccr shell ls`、`ccr shell attach` 和 `ccr shell stop`。在 CLI 按 `Ctrl+]` 可脱离并保留进程。

### v0.33.1 修正内容

- 修复桌面客户端中的网页链接无法打开：HTTP、HTTPS 和邮件链接通过系统默认应用打开，失败时显示提示。
- 后台任务面板新增顶部「运行中」分组，集中展示活跃的 Agent、Shell 和队友任务，避免被已完成任务压到列表后方；完成后回到原类型分类。
- 修复无效模型、无权访问模型等明确 API 错误发生后网页仍显示「正在工作」的问题：同步原始错误并转为失败，不再依赖终端结束通知。
- 兼容终端换行和重复相同错误，避免重复显示历史错误或被迟到的结束通知覆盖失败状态；保留重试、用量等待及重新提交后的恢复逻辑。

### v0.33.0 更新内容

- 侧栏新增 Done（已完成）分组：任务完成后保留待查看状态，查看最新结果后转为 Idle；仍有后台子代理运行的会话继续留在进行中。
- 网页识别终端连接中断与自动重试，显示原始错误及重试次数，不再只显示「正在工作」。
- 修复子代理启动记录中字符串格式的后台标记导致的漏检；主代理结束后，网页、Herdr 和支持 OSC 7501 的终端仍能反映后台子代理的工作状态。
- 优化对话框关闭：返回操作优先关闭弹层，桌面客户端的 Ctrl+W 优先关闭当前对话框。
- 新增本地 Claude Code / Codex 供应商配置、连接测试、模型发现及 CC-Switch 配置导入，网页和命令行均可管理；凭据使用只写字段保护。
- 调整设置导航与供应商编辑流程，支持原生设置文档及状态栏配置；状态栏新增短标签、配色与预览，桌面用量托盘同步优化。

### v0.32.2 修正内容

- 将 Claude Code 用量上限菜单转换为结构化 `UsageLimit` Tool，在网页直接选择停止等待、重置后自动继续、切换用量额度或升级套餐，不再展示原始终端文本。
- 提交时校验当前菜单并按终端实时选中项执行选择；拦截失效菜单与重复提交，兼容终端先作答和菜单重绘。
- 对话保留 Tool 处理记录；自动等待重置期间保持等待状态，排队消息不会误触菜单。

### v0.32.1 修正内容

- 修复 Claude Code 达到用量上限并等待菜单选择时，网页仍显示「正在工作」的问题；现在显示用量限制、恢复时间和原始选项，并提供终端入口选择处理方式。
- 用量上限菜单显示期间暂停提交排队消息，避免消息误触菜单选项；菜单关闭后恢复正常状态。
- 单条立即发送的消息直接显示在对话中，避免误显示为排队消息。
- 侧栏新增新建会话入口和批量归档操作，并更新项目功能说明。

### v0.32.0 更新内容

- 桌面端新增多服务器标签页，每个标签页独立保留连接、页面和会话状态；支持拖动排序、中键关闭、恢复关闭的标签和键盘切换。
- 重构为单行自定义标题栏，集中显示应用版本、前进、后退、刷新、标签页和窗口按钮；标题及空白区域支持拖动、双击最大化或还原，左侧标题单击不再打开关于页面。
- 「关于与更新」集中显示当前版本、更新检查、下载进度和发布说明，可从窗口菜单、设置或托盘进入，未连接服务器时也可使用。
- 隔离各标签页的 HTTP 请求和终端连接，关闭或刷新标签页时释放其资源；同步共享偏好时保留各标签页的布局。

### v0.31.0 更新内容

- Windows x64 桌面端支持应用内更新：启动后自动检查最新正式版，并提供设置页和托盘菜单的手动检查入口。
- 更新显示下载进度，确认后安装并重启桌面客户端；签名和绑定版本校验阻止被篡改的更新包。后台服务继续独立运行和更新。
- 发布流水线生成签名安装包与 `latest.json`，兼容完整安装入口和便携目录。旧版客户端需先手动升级到此版本，后续版本可在应用内更新。

### v0.30.2 更新内容

- 更新中英文 README，按平台整理一键安装、首次连接、会话接力和更新指引，补充双语入门指南。
- 新增双语项目介绍页 `/about/` 和 `/about/en/`，随稳定版部署到 GitHub Pages；原路径继续提供网页客户端。
- 应用图标改为间距清晰的「CC」，统一网页、PWA、桌面客户端及托盘资源；宿主默认图标同步更新，保留自选图标。

### v0.30.1 修正内容

- 修复 Windows 会话及供应商选择器中的方向键输入；`/` 筛选时不再把方向键显示成 `[A`、`[B`，退出时恢复原始终端设置。
- 普通选择状态新增 `q` 退出；筛选状态保留 `q` 作为搜索文字，先按 Esc 返回后可退出。纯文本编号模式支持输入 `q` 后回车退出。
- 会话和供应商列表统一编号列宽、第二行悬挂缩进，选中项两行一起高亮；多位编号及筛选后的正文保持对齐。

### v0.30.0 升级提醒

- `ccr` 和 `cc-remote` 现在共用入口：无参数均进入当前项目会话选择，查看帮助请使用 `--help`。前台运行后台改用 `ccr serve`；现有明确的宿主启动参数和已安装服务调用仍兼容。
- `status`、`config` 等管理命令也是会话标题时，使用 `ccr session status` 或 `ccr attach status` 接入。`--resume`、`-r`、`-c` 及旧启动器继续支持。
- Windows 桌面客户端内置原生用量托盘，旧独立托盘已移出本仓库，不再单独发布。完整安装脚本会更新后台和桌面客户端；托盘退出并重新打开后使用新版本。
- 供应商用量摘要读取当前账号已有缓存；需要升级后台才能显示新增摘要，旧后台仍保留供应商选择功能。

### 前后端兼容约定

独立网页（含 GitHub Pages）与宿主服务可以分别升级。纯前端更新必须兼容旧服务端，不能要求用户同时升级宿主：

- 新增字段按可选处理，缺失时安全降级；未知时间、统计或额度不能伪装成零值或“没有对话”。
- 新增提供方和 API 必须检测宿主能力；旧宿主不支持的操作不可发送，尤其不能让旧宿主忽略参数后执行错误的提供方。
- 新接口缺失或旧响应不含新字段时，保留已有会话浏览和 Claude 设置等基本流程；发布前补齐旧响应与缺失接口的回归验证。

```sh
git tag v0.2.0
git push origin v0.2.0
```

- 版本号必须是语义化版本：`vX.Y.Z` 或预发布 `vX.Y.Z-rc.1`。带 `-` 的标签自动标记为 prerelease；prerelease 不会被「最新版本」安装/更新选中，需要显式指定 `--version`。
- 也可以在 Actions 页手动运行 **Release** 工作流（workflow_dispatch），输入 `v0.2.0`；只允许在标签或 `main` 上运行，标签不存在时会在所选提交上创建。
- 同名 Release 已存在时工作流**直接失败**，不会覆盖已发布的附件（安装脚本据此校验）。确需重传附件时，手动运行并勾选 `replace`。
- 发布前在本文添加对应的 `### v版本号 更新内容` 小节。工作流把该小节用于 GitHub Release 和桌面更新日志。没有对应小节时，GitHub 自动生成发行说明，桌面端提供发行页面链接。

## 工作流

| 工作流 | 触发 | 内容 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | push / PR 到 `main` | ubuntu + windows：在 `web/` 安装锁定依赖、类型检查、构建前端、`go vet ./...`、`go test -race ./...`；另在 ubuntu 上 `CGO_ENABLED=0` 交叉编译 darwin/arm64；`govulncheck ./...` 发现可达漏洞（含标准库）即失败 |
| `.github/workflows/release.yml` | 推送 `v*` 标签、手动（仅标签或 `main`） | 6 个平台后台、独立网页、Windows x64 内置托盘的 Tauri 客户端 → 打包 → `checksums.txt` → 构建来源证明（attestation，仅公开仓库）→ 发布 GitHub Release |
| `.github/workflows/native-windows.yml` | 由 CI / Release 调用 | 统一原生版本、Rust 测试、Tauri NSIS 安装包、包含后台和桌面客户端的便携 ZIP |

Go 版本取自 `go.mod`：`go` 指令是最低语言版本，`toolchain` 指令固定实际编译用的补丁版本（含标准库安全修复）；`actions/setup-go` 使用 `go-version-file: go.mod` + `check-latest: true` 读取它。升级工具链时修改 `toolchain` 一行即可。

### 校验来源证明（可选）

Release 工作流用 `actions/attest-build-provenance` 为归档和 `checksums.txt` 生成 SLSA 来源证明，可确认文件由本仓库的工作流构建：

```sh
gh attestation verify cc-remote_0.2.0_linux_amd64.tar.gz -R huanfeng/cc-remote
```

GitHub 不为个人账户下的私有仓库提供 artifact attestations（实测返回 "Feature not available for user-owned private repositories"），因此该步骤仅在仓库公开时运行；私有期间只能依赖 `checksums.txt` 校验。

### 发布附件

| 文件 | 内容 |
| --- | --- |
| `cc-remote-desktop_<版本>_windows_amd64-setup.exe` | Windows x64 Tauri 客户端 NSIS 安装包；后台需另外安装 |
| `cc-remote-desktop_<版本>_windows_amd64-setup.exe.sig` / `latest.json` | 桌面更新签名与更新清单；安装前验证签名及绑定版本 |
| `cc-remote-desktop_<版本>_windows_amd64.zip` | Windows x64 便携包：后台、内置托盘的 Tauri 客户端、使用说明和许可证 |
| `cc-remote_<版本>_windows_amd64.zip` / `_windows_arm64.zip` | `cc-remote.exe`、`README.md`、`LICENSE` |
| `cc-remote_<版本>_linux_{amd64,arm64}.tar.gz` | `cc-remote`（可执行）、`README.md`、`LICENSE` |
| `cc-remote_<版本>_darwin_{amd64,arm64}.tar.gz` | 同上 |
| `cc-remote_<版本>_web.zip` | 独立网页前端（静态文件），见[独立网页前端](standalone.md)；正式版同时发布到 GitHub Pages（`.github/workflows/pages.yml`）；`cc-remote update` 不会选它 |
| `checksums.txt` | 所有归档及桌面安装包的 sha256（`sha256sum` 格式） |
| `install.ps1` / `install.sh` | 安装脚本副本；`releases/latest/download/install.*` 即指向最新正式版中的这两个文件 |
| `install-desktop.ps1` | Windows x64 完整安装入口：后台服务和内置托盘的 Tauri 客户端；从同一份 PowerShell 安装逻辑生成 |

`<版本>` 不带 `v`，例如标签 `v0.2.0` 对应 `cc-remote_0.2.0_linux_amd64.tar.gz`，程序内的版本号（`main.version`）为 `0.2.0`。

### 桌面与托盘流水线

`.github/workflows/native-windows.yml` 是 CI / Release 共用的 Windows x64 构建任务：锁定 pnpm / Cargo 依赖安装，统一 Tauri 配置、桌面 Cargo 包版本和 lockfile，构建桌面前端，运行桌面 Rust 测试，再构建 NSIS 安装包和便携 ZIP。PR / push 到 `main` 使用 `0.0.0-ci`，附件位于 Actions 的 `native-windows-amd64`，保留 7 天。

Release 使用标签对应的版本；原生任务成功后才发布，避免只发布后台的半成品版本。安装包和 ZIP 与原有后台、独立网页一起进入同一个 Release、校验清单和来源证明。后台安装脚本和 `cc-remote update` 继续按 `cc-remote_` 前缀选择后台归档。

当前原生发布范围为 Windows x64。桌面客户端支持签名校验的应用内更新（包括内置托盘），启动 15 秒后检查最新正式版，此后每六小时检查，用户确认后下载安装并重启客户端。「关于与更新」也可更新已注册为服务的本机 Core，Core 在会话空闲后重启。完整安装入口和 `cc-remote update` 仍可使用。Windows ARM64、macOS 和 Linux 暂不发布原生客户端。更新签名与 Windows Authenticode 代码签名不同，当前仍未配置后者。

### 首次启用桌面自动更新

按照 [Tauri 官方更新说明](https://v2.tauri.app/plugin/updater/)生成一对长期保留的密钥，在 `desktop/` 运行（将输出路径替换为仓库外的安全备份目录）：

```powershell
pnpm tauri signer generate -w C:\secure-backup\cc-remote-updater.key
```

在 GitHub 仓库 Settings → Secrets and variables → Actions 中配置：

- Secret `TAURI_SIGNING_PRIVATE_KEY`：私钥文件的完整内容，不是本机文件路径。
- Secret `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`：生成私钥时的密码；无密码可不设置。
- Variable `TAURI_UPDATER_PUBLIC_KEY`：`.key.pub` 文件的完整内容（Tauri 输出的 base64 文本）。

私钥和密码不能提交到 Git；请独立备份。客户端信任发布时嵌入的公钥，后续发布必须继续使用同一密钥对。Release 缺失配置时会失败，普通 CI 与本地无密钥构建仍可运行，但手动检查会说明该构建未启用更新验证。

流水线生成临时 `updater.release.json` 配置，启用 `createUpdaterArtifacts` 和 `requireSignedVersion`，随 NSIS 安装包发布同名 `.exe.sig` 和 `latest.json`。清单使用 `windows-x86_64` 平台键、签名文件内容和带版本的安装包 URL；签名同时绑定版本，防止清单把旧安装包冒充新版本。检查地址为当前仓库的 `releases/latest/download/latest.json`，预发布不会进入此通道。

已有旧版客户端没有自动更新入口，需先通过完整安装脚本或新版安装包升级一次。脚本安装目录、自定义目录和便携目录都会作为安装目标传给 NSIS；便携客户端首次应用内更新会转换为 NSIS 管理的安装。更新只替换客户端，保留宿主后台、账号和历史，安装时客户端短暂退出。纯托盘模式也会检查并发出系统通知，可从托盘菜单「检查桌面端更新」打开安装提示。

首次签名发布后，应在测试机器上验证一次从该版本到下一签名版本的完整检查、下载、安装与重启；本地单元测试无法代替真实跨版本安装。

## 凭据

安装脚本和 `cc-remote update` 按以下顺序选择下载方式：

1. **GitHub CLI**：已安装且 `gh auth login` 登录 → 用 `gh release view/download`。
2. **令牌**：环境变量 `GITHUB_TOKEN` 或 `GH_TOKEN` → GitHub REST API（`/releases/latest`、`/releases/tags/<tag>`、`/releases/assets/<id>`）。
3. 都没有 → 匿名访问 REST API（公开仓库的默认路径），失败时提示如何认证。

仓库私有时（或 fork 到私有仓库并用 `CC_REMOTE_REPO` 指向它）需要凭据，推荐 **fine-grained personal access token**：Repository access 只选目标仓库，Permissions → **Contents: Read-only**。classic token 需要 `repo` scope。

## 一键安装 / 更新

重复执行同一命令就是**更新**：脚本下载新版本、校验 SHA256，然后运行新二进制的 `service install`，替换后台并请求会话空闲后重启；已有的 `--listen` 等配置不传参数时保留。默认同时创建 `ccr` 别名。完整安装入口同时更新客户端和托盘，后台专用入口只更新后台。

### Windows（PowerShell 5.1 / 7）

Windows x64 完整安装（后台、客户端、托盘）：

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1 | iex
```

默认安装到 `%LOCALAPPDATA%\cc-remote\bin`，创建开始菜单中的 **CC Remote** 和 **CC Remote Tray**（同一个 desktop 程序，后者带 `--tray`）。安装完成后以 `--tray` 启动此目录的 desktop，常驻及 Windows 原生悬停不创建 WebView；悬停用量窗口由 wind-ui-rust 原生渲染，点击托盘时才创建主窗口 WebView。更新时旧独立托盘的登录自启快捷方式会改指向 desktop `--tray`，旧偏好及数据保留。运行中的客户端可继续使用，从托盘菜单退出后重新打开即使用新版。更新保留后台配置、账号、历史、客户端存储和托盘偏好。

可选登录自启或只安装不启动：

```powershell
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1))) -AutoStart
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install-desktop.ps1))) -NoLaunch
```

`-AutoStart` 创建当前用户的 Startup 快捷方式，后续更新会保留；不加此参数不会新建自启项。要关闭自启，可在 `shell:startup` 中删除 **CC Remote Tray** 快捷方式。

仅安装后台（Windows x64 / ARM64，保留原有入口）：

```powershell
irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1 | iex
```

需要参数时（`iex` 无法传参，改用 scriptblock）：

```powershell
& ([scriptblock]::Create((irm https://github.com/huanfeng/cc-remote/releases/latest/download/install.ps1))) -Listen lan:8787 -Version v0.2.0
```

私有仓库可改用已登录的 gh 获取脚本：

```powershell
$s = gh api repos/huanfeng/cc-remote/contents/scripts/install.ps1 -H "Accept: application/vnd.github.raw" | Out-String
& ([scriptblock]::Create($s)) -Listen lan:8787
```

参数：

| 参数 | 说明 |
| --- | --- |
| `-Version v0.2.0` | 指定版本，默认最新正式版 |
| `-Listen a,b` | 传给 `service install --listen`，可重复或逗号分隔 |
| `-Lang zh` | 界面语言 `auto` / `zh` / `en`，传给 `service install --lang`；默认取 `$env:CC_REMOTE_LANG`，都未设置时沿用已安装的设置 |
| `-NoService` | 不装服务，只把所选包的程序复制到 `-InstallDir`（默认 `%LOCALAPPDATA%\cc-remote\bin`）；完整包仍创建普通启动与托盘模式快捷方式 |
| `-InstallDir DIR` | 客户端和托盘的目标目录；只有 `-NoService` 时后台也复制到此处，服务模式的后台仍安装在标准目录。自定义目录更新时需再次传入 |
| `-AutoStart` | 完整安装入口：创建客户端 `--tray` 模式的当前用户登录自启快捷方式；更新保留已有自启项 |
| `-NoLaunch` | 完整安装入口：不启动或重启托盘，适合只安装或无交互环境 |
| `-NoAlias` / `-Alias:$false` | 不创建 `ccr` 别名（默认创建） |
| `-DryRun` | 只解析版本和附件并打印计划，不下载不安装 |

服务模式安装到 `%LOCALAPPDATA%\cc-remote\bin\cc-remote.exe`。

### Linux / macOS

```sh
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh
# 带参数
curl -fsSL https://github.com/huanfeng/cc-remote/releases/latest/download/install.sh | sh -s -- --listen lan:8787
```

私有仓库可改用 gh 或令牌获取脚本：

```sh
gh api repos/huanfeng/cc-remote/contents/scripts/install.sh -H "Accept: application/vnd.github.raw" | sh
# 或
export GITHUB_TOKEN=<token>
curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" -H "Accept: application/vnd.github.raw" \
  https://api.github.com/repos/huanfeng/cc-remote/contents/scripts/install.sh | sh -s -- --listen lan:8787
```

参数：`--version vX.Y.Z`、`--listen ADDR`（可重复/逗号分隔）、`--lang auto|zh|en`（默认取 `CC_REMOTE_LANG`，都未设置时沿用已安装的设置）、`--no-service`、`--dir DIR`（`--no-service` 的目标，默认 `~/.local/bin`）、`--no-alias`、`--dry-run`。需要 `curl`（或 `wget`）、`tar`、`sha256sum`/`shasum`；使用令牌下载时必须有 `curl`（附件下载会重定向到存储主机，只有 curl 分支能保证令牌不随重定向发出）。

不传 `--listen` 重新运行即为更新：沿用已安装服务的数据目录、监听地址和 Claude 设置。`--listen` 绑定所有网卡（`all:`/`0.0.0.0`）会被拒绝，需要时请直接运行 `cc-remote service install --listen all:8787 --allow-insecure-remote`。

服务模式安装位置：Linux `~/.local/share/cc-remote/bin/cc-remote`（systemd 用户服务），macOS `~/Library/Application Support/cc-remote/bin/cc-remote`（launchd）。

### 环境变量

| 变量 | 作用 |
| --- | --- |
| `GITHUB_TOKEN` / `GH_TOKEN` | 未登录 gh 时使用的令牌 |
| `CC_REMOTE_REPO` | 覆盖仓库，默认 `huanfeng/cc-remote` |
| `CC_REMOTE_API_URL` | 覆盖 API 地址（脚本测试/GHE 用），默认 `https://api.github.com` |

## 从界面更新组件

Windows 桌面端的「关于与更新」显示客户端和本机 Core 的版本及更新日志。更新桌面客户端时会重新打开客户端。更新本机 Core 时，客户端保持打开。

连接目标服务器后，打开「设置 → 服务器 → 组件更新」。CC Remote Core、Claude Code 和 Codex 可以分别检查并更新。Core 会在会话空闲后重启，桌面客户端保持打开。宿主会保存最近 30 次更新结果和可用的发布说明链接。

- **CC Remote Core** 需要通过 CC Remote 注册为服务。安装前会校验发布包的 SHA-256。
- **Claude Code 和 Codex** 支持官方独立安装和可识别的 npm 全局安装。独立安装使用各自的更新器。npm 安装使用检查得到的具体版本。通过系统包管理器或自定义路径安装时，仍由原安装方式处理。
- 更新 Claude Code 或 Codex 会等该组件的活动轮次和打开的终端会话结束。更新完成后，新轮次使用新版 CLI。

旧版宿主需要先使用安装脚本或命令行更新一次，之后才能在此界面检查和更新组件。

## 用 cc-remote 自身更新

以下命令用于更新后台。客户端和托盘可在「关于与更新」中升级，也可以重新执行 `install-desktop.ps1` 完整安装入口。

```sh
cc-remote update --check          # 只查看是否有新版本
cc-remote update                  # 有新版本时安装
cc-remote update --version v0.2.0 # 安装指定版本
cc-remote update --yes            # dev 构建、重装同版本或降级时需要
cc-remote update --force          # 立即重启服务，不等会话空闲
```

流程同安装脚本：取凭据 → 下载与当前平台匹配的归档和 `checksums.txt` → 校验 sha256 → 解压。若已安装服务（存在固定位置的二进制），执行新二进制的 `service install` 完成替换和重启——不带 `--data-dir`，因此沿用安装目录中 `service.json` 副本记录的数据目录和设置；否则直接替换当前可执行文件（Windows 先把运行中的 exe 改名为 `.old`；旧 `.old` 仍被占用时改名为 `.old-<时间戳>`，下次运行 `update` 时清理所有 `.old*`）。

默认不停止正在运行的服务（`service install` 同样如此）：只替换程序（运行中的 exe 先改名），然后请求宿主在没有会话执行中、等待回答或运行后台任务（托管会话和终端会话的子 agent、后台命令）时自动重启。`--force` 改为立即停止并重启；`--when-idle` 是旧版本的参数，现在是默认行为，仍可传入。宿主以退出码 76 退出，由服务管理器（Windows 计划任务的 supervisor、systemd、launchd）重新启动并运行新版本。运行中的宿主版本较旧（v0.5 之前）、不支持自动重启时，按原来的方式立即停止并重启。安装 v0.10.0 之前的版本时，`--force` 不传给它（旧版本默认就是立即重启）。宿主停止前把正在运行的终端会话记在数据目录的 `restore.json`，重启后自动以 `--resume` 恢复它们（只尝试一次），接入中的 `ccr` 等待宿主恢复后自动重新接入。

查询与下载有超时：连接、TLS 握手和等待响应头各 30 秒，整个查询+下载最长 10 分钟。匿名访问时，若仓库本身可读而指定标签不存在，报告 `release vX.Y.Z not found`；只有仓库不可访问（私有且未认证）时才显示认证帮助。

## 服务管理速查

```sh
cc-remote service status     # 状态
cc-remote service start|stop
cc-remote service logs
cc-remote service config --listen lan:8787   # 无需重启；其它设置空闲时重启生效，--force 立即重启
cc-remote service uninstall
cc-remote alias install      # 创建 ccr
```

完整说明见 [configuration.md](configuration.md#后台服务) 和 `cc-remote service --help`。

## 卸载

```sh
cc-remote service uninstall                  # 保留副本和数据目录
cc-remote service uninstall --purge-binary   # 同时删除副本目录并从用户 PATH 移除它
cc-remote alias uninstall                    # 删除 ccr 并移除其 PATH 条目
```

之后删除安装目录（Windows `%LOCALAPPDATA%\cc-remote`，Linux `~/.local/share/cc-remote`，macOS `~/Library/Application Support/cc-remote`）。数据目录（令牌、SQLite）默认在 Windows `%APPDATA%\cc-remote`、Linux `~/.config/cc-remote`、macOS `~/Library/Application Support/cc-remote`，需要时手动删除。

`cc-remote service uninstall` 只卸载后台服务，不会删除客户端或快捷方式。从托盘菜单退出客户端后，可手动删除安装目录里的 `cc-remote-desktop.exe`、开始菜单中的 `CC Remote` 文件夹（`%APPDATA%\Microsoft\Windows\Start Menu\Programs\CC Remote`），以及可选的 Startup 自启项 `CC Remote Tray.lnk`（运行 `shell:startup` 打开）。旧版遗留的 `cc-remote-tray.exe` 可在退出该程序后一并删除。这些操作保留后台账号与历史、客户端 WebView 存储和托盘偏好。

## 故障排查

- **`cannot access releases` / 404**：仓库私有而未认证，或匿名访问触发了 API 速率限制。运行 `gh auth status`；或检查令牌是否覆盖本仓库且有 Contents: Read。fine-grained token 若属于组织仓库，可能需要组织管理员批准。
- **`no release found`**：仓库还没有正式 Release，或只有 prerelease（需 `--version` 指定）。
- **401/403**：令牌过期、权限不足，或触发了 API 速率限制。
- **sha256 mismatch**：下载不完整或被篡改，脚本已中止且未改动现有安装，重试即可。
- **macOS Gatekeeper**：二进制未签名。通过脚本/curl 下载一般不带隔离属性；若从浏览器下载后被拦截，执行 `xattr -d com.apple.quarantine <路径>/cc-remote`。
- **Windows SmartScreen / 「已阻止」**：安装脚本会自动 `Unblock-File`；手动下载的 zip 先执行 `Unblock-File .\cc-remote_*.zip` 或在文件属性里勾选「解除锁定」。执行策略拦截脚本时使用上文的 `Invoke-Expression` / scriptblock 方式，或 `powershell -ExecutionPolicy Bypass -File install.ps1`。
- **Linux 注销后服务停止**：systemd 用户服务默认随会话退出，执行 `loginctl enable-linger $USER`。
- **Windows 更新后旧文件残留**：`cc-remote.exe.old` / `.old-<时间戳>` 是被替换的旧版本，下次 `update` 自动删除（仍在运行的会保留到下次），也可手动删除。
- **服务中的会话报 `API Error: 403 Request not allowed`，终端直接运行 `claude` 正常**：代理只写在 shell 配置（如 PowerShell `$PROFILE`）里，服务不加载它而直连。执行 `cc-remote service config --env HTTPS_PROXY=http://127.0.0.1:7890`（或在已设代理的终端执行 `cc-remote service config --capture-env`），`cc-remote service status` 的 `Env:` 行可确认。
- **服务反复启动失败、`host.log` 提示 giving up**：数据目录锁或端口被另一个 cc-remote 占用（宿主退出码 75）。停止另一个宿主后执行 `cc-remote service start`。
