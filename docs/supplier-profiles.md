# CC-Switch 供应商选择

## 本地供应商配置

新版设置的「供应商配置」支持直接添加 Claude Code / Codex API 供应商，并保留原有 OAuth 账号管理及 CC-Switch 只读导入。API 供应商可配置名称、HTTP(S) 端点、默认模型和认证方式；Claude 支持 API Key / Auth Token，新增自定义配置默认使用 Auth Token（网页和命令行一致），官方 Anthropic 模板使用 API Key，编辑时保留原认证方式。Codex 使用 API Key 与 Responses API。

编辑已有配置时凭据框始终为空，留空表示保留原凭据；列表及保存响应仅返回“已配置凭据”状态。端点不能包含用户名、密码、查询参数或片段。授权信息保存在宿主数据目录的 `supplier-configs.json`，不会写入本地供应商会话快照或回传到设置页。

本地供应商的修改会在托管会话下次安全启动时读取；正在工作的请求不会被立即中断。删除配置后，引用它的会话需要重新选择供应商；不会悄悄回退至其它账号。终端进程需要重新启动或切换供应商后才会使用新配置。旧服务端仍可管理原有 OAuth，新增 API 供应商功能需要更新宿主。

cc-remote 直接只读读取**宿主机器上**的 CC-Switch SQLite 数据库。不再使用 cc-tool，也不需要先导出供应商 JSON。

默认路径：Windows `%USERPROFILE%\.cc-switch\cc-switch.db`，Linux `~/.cc-switch/cc-switch.db`。

前台启动可指定数据库：

```powershell
cc-remote --cc-switch-db "D:\Config\cc-switch.db"
```

也可使用环境变量 `CC_REMOTE_CC_SWITCH_DB`。后台服务使用已有环境配置入口：

```powershell
cc-remote service config --env "CC_REMOTE_CC_SWITCH_DB=D:\Config\cc-switch.db"
```

## 使用

- Web 新建会话：选择 Claude Code 或 Codex，再选择对应供应商。默认使用宿主设置的新会话供应商，也可明确选择“宿主原生配置”。
- Web 原生历史：Claude 会话可用终端继续或导入托管会话，确认时可选择沿用原配置。Codex 原生历史可导入托管会话，供应商在导入时选择。
- `ccr claude --new`：交互终端会列出 Claude 供应商；`ccr claude resume` / `ccr claude resume --last` 在恢复时同样可以选择，回车沿用原配置。
- `ccr claude --new --supplier <供应商ID>` 或 `ccr claude resume <会话ID> --supplier <供应商ID>`：指定供应商。支持数据库原 ID 或列表中的 `ccswitch:` ID。
- `ccr codex --new`：从与网页相同的供应商目录选择，回车采用新会话默认。也可用 `ccr codex --new --supplier <供应商ID>` 直接指定。
- Codex 终端恢复使用原会话记录的供应商。指定不同的供应商或原供应商不可用时，宿主会报错。尚未登记且没有供应商记录的 Codex 原生历史默认使用宿主配置，也可以用 `--supplier <供应商ID>` 显式指定。
- `--supplier @native`：明确使用宿主原生配置。Claude 非交互恢复且没有指定 `--supplier` 时保持原会话配置。
- Claude 和托管 Codex 会话可在设置中切换供应商；忙碌时需要确认中断，保留原会话 ID 和历史，不自动重发请求。Codex 终端的供应商在会话状态和详情中可查看。

## 命令行管理本地供应商

无需安装 CC-Switch 即可新增、修改、删除 API 供应商，并使用 `profile default` 切换新会话的默认配置。以下命令的 `ccr` 也可写成 `cc-remote`：

```sh
ccr profile add --app claude --name "My API" --base-url https://api.example.com --auth-type apiKey --model my-model
ccr profile ls --app claude
ccr profile default "My API" --app claude
ccr profile edit "My API" --name "Work API" --base-url https://api.example.com/v1
ccr profile edit "Work API" --model ""
ccr profile edit "Work API" --credential-stdin
ccr profile rm "Work API"
```

新增时通过隐藏输入、环境变量 `CC_REMOTE_SUPPLIER_CREDENTIAL` 或标准输入提供凭据；`--credential-stdin` 强制读取标准输入，不使用环境变量。凭据不支持命令参数，避免留在进程参数和 shell 历史中。编辑默认保留原凭据，只有显式使用 `--credential-stdin` 才读取新凭据，读取为空仍保留原凭据。输出不显示已有或新输入的凭据。

`add` 必须提供 `--name` 与 `--base-url`，默认认证方式为 `apiKey`；Claude 也支持 `--auth-type authToken`。Codex 使用 `--app codex`，仅支持 API Key 与 Responses API。`edit` 仅修改明确提供的字段，`--name` 重命名，`--model ""` 清除默认模型。

编辑、删除支持完整 ID 或对应应用内唯一名称，完整 ID 优先；同名时必须使用 ID。此处仅管理本地 API 配置，OAuth 使用 `ccr oauth`，CC-Switch 导入项在原程序中管理。所有命令支持 `--host`、`--data-dir`；目标宿主不支持此功能时会提示升级。并发修改会明确报错，请重新查看配置后再执行，不自动覆盖他人的更改。

## 默认供应商

新建会话时没指定供应商，用的就是「默认供应商」，网页的新建会话和 `ccr` 完全一致。优先级：明确指定（网页下拉、`--supplier`）> 你设置的默认 > CC-Switch 当前选中的供应商 > 宿主原生配置。

- 设置：网页「设置 → 会话默认 → 新会话供应商」，或命令行：

  ```sh
  cc-remote profile ls                 # 列出供应商，* 标记当前默认
  cc-remote profile default            # 查看默认
  cc-remote profile default 名称       # 指定默认（名称或 id，包括 OAuth 令牌配置）
  cc-remote profile default @native    # 默认用宿主原生配置
  cc-remote profile default follow     # 不单独指定，跟随 CC-Switch 当前项
  ```

  Codex 加 `--app codex`。设置保存在宿主数据目录的 `default-profiles.json`。
- 只影响之后新建的会话；已有会话保持创建时的供应商，恢复时仍可「沿用原配置」。
- 指定的默认供应商被删除或不可用时，新建会话会明确报错，提示重新选择，**不会悄悄换成别的供应商**。
- Claude Code 和 Codex 新建终端时，供应商选择器的回车默认与网页相同。

## OAuth 令牌配置

CC-Switch 的供应商是「整体替换」：端点、密钥、模型变量都换成供应商的。OAuth 令牌配置是另一种东西，它**叠加在宿主原生配置之上**：会话照常使用宿主自己的 Claude Code 配置（模型、插件、hooks、MCP、权限），只是把登录令牌换成你保存的 `CLAUDE_CODE_OAUTH_TOKEN`。适合给不同的官方账号分别建一项。

- 生成令牌：在能登录的机器上运行 `claude setup-token`，得到 `sk-ant-oat…`。
- 添加：网页「设置 → 供应商配置 → Claude 官方 OAuth」。令牌只保存在宿主数据目录的 `oauth-profiles.json`（权限 0600），添加后不会再显示，也不出现在任何列表接口里；请保护该目录。保存失败会显示具体原因，例如令牌包含空格或格式不正确。
- 编辑：可修改名称，或填写新令牌；令牌留空会保留原令牌，条目 ID 保持不变。默认配置和已有会话保存的名称不会自动改写，需要时请手动重新选择保存。
- 选择：与 CC-Switch 供应商出现在同一个选择器里，名称后带「· OAuth」；`ccr` 的供应商列表同样带标记，`--supplier oauth:<ID>` 可直接指定。
- 冲突处理：启动时清空 `ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_API_KEY`、`ANTHROPIC_BASE_URL`（它们优先于 OAuth 令牌，或会把令牌发到别的端点），其余 `ANTHROPIC_*`（包括模型变量）保持原生设置。
- 会话只记引用（`oauth:<ID>`），**每次启动现取令牌**：更换令牌后，使用它的会话下次启动即生效；条目被删除后，这些会话启动时会提示重新选择供应商。
- 命令行：`cc-remote oauth ls | add 名称 | token 名称 | rename 名称 新名称 | rm 名称`（名称也可写 `ls` 显示的 id）。`add` 和 `token` 的令牌从环境变量 `CC_REMOTE_OAUTH_TOKEN`、标准输入或隐藏输入读取，也可用 `--token`（会留在 shell 历史里，不推荐）。命令经由运行中的宿主完成，宿主没运行时会先启动。
- 接口：`POST /api/profiles/oauth`、`PATCH|DELETE /api/profiles/oauth/{ID}`，都需要访问令牌。

- 订阅用量：使用 OAuth 令牌的会话，顶栏额度和用量详情显示**该令牌所属账号**的 5 小时 / 每周额度（与官方登录一样），不再是宿主自己的登录。状态页「用量」处可切换查看宿主登录或任一 OAuth 账号；每个账号有独立的用量历史（宿主每 10 分钟记录一次，删除令牌时一并清除）。令牌失效时会提示更换。

限制：模型列表按宿主配置生成，不随供应商变化。

## 数据与兼容性

每次读取列表和明确选择供应商时重新查询 CC-Switch。会话保存私有配置快照，之后 CC-Switch 默认供应商变化不影响已有会话。API 只返回名称、ID、应用和不可用原因，不返回密钥；快照存于宿主私有数据库，请保护该数据目录。

读取仅依赖 providers 的 id、app_type、name、settings_config，允许额外列和版本变化。可选读取 meta、is_current、sort_index、公共配置和同目录 settings.json 的当前供应商标记。cc-remote 不写入 CC-Switch 数据库，也不修改其全局选中项。

Claude 读取 API 连接环境变量，按 commonConfigEnabled 合并公共配置；不导入权限、hooks 或 MCP。Codex 读取 auth.OPENAI_API_KEY 与 config TOML 的模型和连接字段，支持普通/带引号的表名、标量与独立公共配置。连接字段中的内联表、数组、多行字符串等未支持语法会列为不可用，不猜测解析。官方 OAuth 登录请选择“宿主原生配置”，使用宿主已有登录。

数据库缺失时可使用原生配置；损坏或必要字段缺失时显示明确错误。单个供应商不可用会显示原因，不阻止其它有效供应商使用。

## Windows 与 Linux

列表来自运行 cc-remote **宿主进程**的机器，而非浏览器所在机器。Windows 上的 CC-Switch 不会自动出现在 Linux 宿主上；可将其一致性数据库备份放到 Linux 并指定路径。复制前退出 CC-Switch，或使用 SQLite 备份工具，避免遗漏 WAL 中的更新。数据库包含密钥，请通过可信通道传输并限制访问权限。

Linux 无需运行 CC-Switch GUI。此功能只读取配置、启动和恢复会话，不做跨机数据库同步。

### CLI 高级配置

`ccr profile show 名称 --app claude --json` 查看本地 API 供应商的脱敏详情（包括已配置凭据状态），不返回密钥。

新增和修改共用这些参数，只有显式指定的字段才会修改：

- `--website`、`--notes`：网站与备注。
- Claude：`--haiku-model`、`--sonnet-model`、`--opus-model`、`--timeout-ms`。
- Codex：`--reasoning-effort`、`--reasoning-summary`、`--verbosity`、`--context-window`、`--auto-compact-token-limit`、`--disable-response-storage=true|false`。

例如：`ccr profile edit 工作 --app claude --opus-model 自定义模型 --timeout-ms 120000`。
字符串显式传空可清除，数值设为 0 沿用应用配置。已有凭据始终保留，除非显式使用 `--credential-stdin` 更换。高级参数需要支持高级供应商配置的宿主。


## 模型与 1M 上下文

请求地址旁的“测试”通过模型列表接口检查认证及连通性，不发送生成请求。模型配置标题旁可获取模型列表；映射按 Sonnet、Opus、Haiku、Fable 各占一行，列为模型角色、显示名称、实际请求模型、1M 声明，默认兜底模型位于映射下方、高级选项之前。

显示名称仅改变模型名称展示，实际请求模型填写供应商模型 ID。Fable 及各角色映射、显示名称均随配置保存和 CC-Switch 导入；命令行可用 `--fable-model` 和 `--sonnet-model-name` 等选项修改。

Claude 的每个映射及默认兜底模型均可声明 1M 上下文。仅在模型支持时勾选，保存格式为模型名末尾的 `[1M]`，兼容 CC-Switch 导入；取消勾选会移除后缀，空模型不能勾选。此声明不会让供应商实际不支持的模型获得更大上下文。Codex 仍使用独立的上下文窗口 Token 数配置。

命令行示例（去掉后缀即可取消声明）：

```powershell
ccr profile edit 名称 --app claude --sonnet-model 'model-id[1M]' --model 'model-id[1M]'
```

实现参考：[CC-Switch 模型状态处理](https://github.com/farion1231/cc-switch/blob/main/src/components/providers/forms/hooks/useModelState.ts)、[Claude Code 上下文配置](https://code.claude.com/docs/en/model-config#extended-context)。
