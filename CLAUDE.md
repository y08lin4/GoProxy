# 维护者与 Agent 指南

本文件用于给代码助手或维护者快速了解当前仓库。虽然文件名保留为 `CLAUDE.md`，内容已经改为当前 GoProxy 仓库的通用维护说明。

## 项目概览

GoProxy 是 Go 编写的代理池系统，负责：

- 抓取公开 HTTP/SOCKS5 代理源。
- 导入 Clash/V2Ray 订阅。
- 通过 sing-box 将加密协议节点转换为本地 SOCKS5。
- 验证出口 IP、地理位置、延迟和 HTTPS 能力。
- 对外提供 HTTP 与 SOCKS5 代理端口。
- 通过 WebUI 管理代理池、订阅和配置。

## 当前分支目标

当前重构分支：

```text
refactor/decouple-core
```

目标是逐步把业务流程从具体实现中拆出：

- `internal/domain` 放核心模型。
- `internal/ports` 放边界接口。
- `internal/service` 放业务编排。
- `fetcher`、`validator`、`pool`、`proxy`、`storage`、`webui` 作为具体实现逐步收敛职责。

## 常用命令

```powershell
go test -count=1 ./...
go build -o .bin/proxygo.exe .
.\.bin\proxygo.exe
```

Linux/macOS：

```bash
go test -count=1 ./...
go build -o proxygo .
./proxygo
```

Docker 本地构建：

```bash
docker build -t goproxy:local .
```

## 重要模块

| 路径 | 说明 |
| --- | --- |
| `main.go` | 依赖装配、启动端口、后台任务。 |
| `internal/service/refill_service.go` | 补池流程。 |
| `internal/domain/types.go` | 核心模型。 |
| `internal/ports/` | 服务边界接口。 |
| `internal/geoip/resolver.go` | GeoIP 查询。 |
| `pool/policy.go` | 池策略。 |
| `proxy/selector.go` | 代理选择策略。 |
| `proxy/failure_reporter.go` | 成功/失败记录和下线策略。 |
| `storage/storage.go` | 当前仍较大，后续重点拆分。 |
| `webui/server.go` | 当前仍包含较多业务逻辑，后续 service 化。 |

## 开发约定

- 小步重构，保持外部端口、配置和 API 行为不变。
- 每次修改后运行：

```powershell
gofmt -w <changed-files>
go test -count=1 ./...
git diff --check
```

- 新增业务边界优先放到 `internal/ports`，流程编排放到 `internal/service`。
- 不要让 `validator`、`pool`、`proxy` 互相直接承担对方职责。
- `storage` 的拆分优先保持 schema 不变，只移动方法和接口。

## 当前下一步

建议下一步拆 `storage`：

```text
storage/proxy_repository.go
storage/subscription_repository.go
storage/source_repository.go
storage/stats_repository.go
```

同时为 service 增加更窄的 ports 接口，减少上层依赖整个 `*storage.Storage`。
