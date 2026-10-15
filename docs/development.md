# 开发

## 环境

- Go：`go.mod` 的 `go` 指令为最低版本（1.26），`toolchain` 指令固定实际编译用的补丁版本。
- Node.js 22 和 pnpm：版本见 `web/package.json` 的 `packageManager`，执行 `corepack enable` 即可使用。仅构建前端需要，运行宿主不需要 Node。
- Claude Code：手动联调时需要已登录的近期版本；自动测试不需要。

## 构建

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web run build          # 输出到 web/dist，Go 编译时嵌入
go build -o cc-remote ./cmd/cc-remote
```

Windows 使用 `go build -o cc-remote.exe ./cmd/cc-remote`。修改前端后必须先重新构建 `web/dist`，再重新编译宿主。

注入版本号：`go build -ldflags "-X main.version=1.2.3" ./cmd/cc-remote`（默认 `dev`）。

## 本地运行

为避免影响正在使用的服务，开发时建议用独立的端口和数据目录：

```sh
./cc-remote --listen 127.0.0.1:8790 --data-dir ./.test-data/dev
```

前端开发服务器把 `/api` 代理到 `127.0.0.1:8787`（见 `web/vite.config.ts`，使用其它端口时临时修改这里）：

```sh
pnpm --dir web run dev
```

## 测试

```sh
go vet ./...
go test ./...
go test -race ./...
pnpm --dir web run typecheck
pnpm --dir web run build
cd web && node --test tests/*.test.mjs
```

Go 测试使用假子进程，不消耗模型额度，覆盖事件解析、配置继承、权限桥接、会话并行 / 串行、取消、持久化、重启状态和数据目录独占等。真实 Claude Code 与浏览器的验证记录见 [verification.md](verification.md)，任务与续接记录见 [tasks.md](tasks.md)。

## CI

`.github/workflows/ci.yml` 在 push / PR 到 `main` 时运行：ubuntu 与 windows 上安装前端依赖、typecheck、构建、前端单元测试、`go vet`、`go test -race`；另在 ubuntu 上 `CGO_ENABLED=0` 交叉编译 darwin/arm64，并运行 `govulncheck`（发现可达漏洞即失败）。

## 发布

推送 `vX.Y.Z` 标签即触发 `.github/workflows/release.yml`，构建 6 个平台的归档、`checksums.txt` 和安装脚本并发布 GitHub Release。完整流程见 [release.md](release.md)。

## 目录结构

见 [architecture.md](architecture.md#总体结构)。
