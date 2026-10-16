# 发布、安装与更新

本文说明如何发布 cc-remote 版本，以及在 Windows / Linux / macOS 上安装、更新和卸载。公开仓库可匿名下载；GitHub 凭据只在仓库私有或遇到 API 速率限制时需要。

## 发布新版本

```sh
git tag v0.2.0
git push origin v0.2.0
```

- 版本号必须是语义化版本：`vX.Y.Z` 或预发布 `vX.Y.Z-rc.1`。带 `-` 的标签自动标记为 prerelease；prerelease 不会被「最新版本」安装/更新选中，需要显式指定 `--version`。
- 也可以在 Actions 页手动运行 **Release** 工作流（workflow_dispatch），输入 `v0.2.0`；只允许在标签或 `main` 上运行，标签不存在时会在所选提交上创建。
- 同名 Release 已存在时工作流**直接失败**，不会覆盖已发布的附件（安装脚本据此校验）。确需重传附件时，手动运行并勾选 `replace`。

## 工作流

| 工作流 | 触发 | 内容 |
| --- | --- | --- |
| `.github/workflows/ci.yml` | push / PR 到 `main` | ubuntu + windows：`pnpm --dir web install --frozen-lockfile`、`typecheck`、构建前端、`go vet ./...`、`go test -race ./...`；另在 ubuntu 上 `CGO_ENABLED=0` 交叉编译 darwin/arm64；`govulncheck ./...` 发现可达漏洞（含标准库）即失败 |
| `.github/workflows/release.yml` | 推送 `v*` 标签、手动（仅标签或 `main`） | 构建一次前端 → 6 个平台 `CGO_ENABLED=0` 编译（`-trimpath -ldflags "-s -w -X main.version=<版本>"`）→ 打包 → `checksums.txt` → 构建来源证明（attestation，仅公开仓库）→ `gh release create --generate-notes` |

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
| `cc-remote_<版本>_windows_amd64.zip` / `_windows_arm64.zip` | `cc-remote.exe`、`README.md`、`LICENSE` |
| `cc-remote_<版本>_linux_{amd64,arm64}.tar.gz` | `cc-remote`（可执行）、`README.md`、`LICENSE` |
| `cc-remote_<版本>_darwin_{amd64,arm64}.tar.gz` | 同上 |
| `checksums.txt` | 所有归档的 sha256（`sha256sum` 格式） |
| `install.ps1` / `install.sh` | 安装脚本副本；`releases/latest/download/install.*` 即指向最新正式版中的这两个文件 |

`<版本>` 不带 `v`，例如标签 `v0.2.0` 对应 `cc-remote_0.2.0_linux_amd64.tar.gz`，程序内的版本号（`main.version`）为 `0.2.0`。

## 凭据

安装脚本和 `cc-remote update` 按以下顺序选择下载方式：

1. **GitHub CLI**：已安装且 `gh auth login` 登录 → 用 `gh release view/download`。
2. **令牌**：环境变量 `GITHUB_TOKEN` 或 `GH_TOKEN` → GitHub REST API（`/releases/latest`、`/releases/tags/<tag>`、`/releases/assets/<id>`）。
3. 都没有 → 匿名访问 REST API（公开仓库的默认路径），失败时提示如何认证。

仓库私有时（或 fork 到私有仓库并用 `CC_REMOTE_REPO` 指向它）需要凭据，推荐 **fine-grained personal access token**：Repository access 只选目标仓库，Permissions → **Contents: Read-only**。classic token 需要 `repo` scope。

## 一键安装 / 更新

重复执行同一命令就是**更新**：脚本下载新版本、校验 sha256，然后运行新二进制的 `service install`，由它停止服务 → 复制到固定位置 → 重新启动；已有的 `--listen` 等配置不传参数时保留。默认同时创建 `ccr` 别名。

### Windows（PowerShell 5.1 / 7）

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
| `-NoService` | 不装服务，只把 exe 复制到 `-InstallDir`（默认 `%LOCALAPPDATA%\cc-remote\bin`） |
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

参数：`--version vX.Y.Z`、`--listen ADDR`（可重复/逗号分隔）、`--no-service`、`--dir DIR`（`--no-service` 的目标，默认 `~/.local/bin`）、`--no-alias`、`--dry-run`。需要 `curl`（或 `wget`）、`tar`、`sha256sum`/`shasum`；使用令牌下载时必须有 `curl`（附件下载会重定向到存储主机，只有 curl 分支能保证令牌不随重定向发出）。

不传 `--listen` 重新运行即为更新：沿用已安装服务的数据目录、监听地址和 Claude 设置。`--listen` 绑定所有网卡（`all:`/`0.0.0.0`）会被拒绝，需要时请直接运行 `cc-remote service install --listen all:8787 --allow-insecure-remote`。

服务模式安装位置：Linux `~/.local/share/cc-remote/bin/cc-remote`（systemd 用户服务），macOS `~/Library/Application Support/cc-remote/bin/cc-remote`（launchd）。

### 环境变量

| 变量 | 作用 |
| --- | --- |
| `GITHUB_TOKEN` / `GH_TOKEN` | 未登录 gh 时使用的令牌 |
| `CC_REMOTE_REPO` | 覆盖仓库，默认 `huanfeng/cc-remote` |
| `CC_REMOTE_API_URL` | 覆盖 API 地址（脚本测试/GHE 用），默认 `https://api.github.com` |

## 用 cc-remote 自身更新

```sh
cc-remote update --check          # 只查看是否有新版本
cc-remote update                  # 有新版本时安装
cc-remote update --version v0.2.0 # 安装指定版本
cc-remote update --yes            # dev 构建、重装同版本或降级时需要
cc-remote update --when-idle      # 不停服务：先替换程序，等所有会话空闲后宿主自动重启
```

流程同安装脚本：取凭据 → 下载与当前平台匹配的归档和 `checksums.txt` → 校验 sha256 → 解压。若已安装服务（存在固定位置的二进制），执行新二进制的 `service install` 完成停服务/替换/启动——不带 `--data-dir`，因此沿用安装目录中 `service.json` 副本记录的数据目录和设置；否则直接替换当前可执行文件（Windows 先把运行中的 exe 改名为 `.old`；旧 `.old` 仍被占用时改名为 `.old-<时间戳>`，下次运行 `update` 时清理所有 `.old*`）。

`--when-idle`（同样适用于 `service install --when-idle`）不停止正在运行的服务：只替换程序（运行中的 exe 先改名），然后请求宿主在没有会话执行中、等待回答或运行后台任务时自动重启。宿主以退出码 76 退出，由服务管理器（Windows 计划任务的 supervisor、systemd、launchd）重新启动并运行新版本。运行中的宿主版本较旧、不支持自动重启时，会提示空闲时手动运行 `cc-remote service restart`。

查询与下载有超时：连接、TLS 握手和等待响应头各 30 秒，整个查询+下载最长 10 分钟。匿名访问时，若仓库本身可读而指定标签不存在，报告 `release vX.Y.Z not found`；只有仓库不可访问（私有且未认证）时才显示认证帮助。

## 服务管理速查

```sh
cc-remote service status     # 状态
cc-remote service start|stop
cc-remote service logs
cc-remote service config --listen lan:8787
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
