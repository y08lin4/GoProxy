# GoProxy 解耦重构计划

本文只描述当前仓库的重构路线，不再引用外部项目计划。

## 总目标

把 GoProxy 从“功能都能跑但模块互相知道太多”的结构，逐步改成：

```text
main / cmd
  只负责装配和生命周期

service
  编排业务流程

domain + ports
  定义核心模型和边界

adapter
  fetcher / validator / storage / proxy / webui 等具体实现
```

## 已完成阶段

### 第 1 阶段：补池流程服务化

提交：`849ce48 重构补池流程并增加验证取消`

- 新增 `internal/service/refill_service.go`。
- 从 `main.go` 抽出补池流程。
- 验证流支持 `context.Context` 取消。
- 新增 `MaxCandidatesPerSource` 控制每个源的候选数量。

### 第 2 阶段：抽核心模型和 ports

提交：`b78512a 抽取核心领域模型和补池端口`

- 新增 `internal/domain/types.go`。
- 新增 `internal/ports/refill.go`。
- `storage.Proxy`、`validator.Result`、`pool.PoolStatus`、`fetcher.Source` 改成类型别名，保留兼容。
- `RefillService` 依赖接口而非具体包。

### 第 3 阶段：GeoIP 解耦

提交：`81ff534 拆分 GeoIP 解析与验证器`

- 新增 `internal/geoip/resolver.go`。
- 新增 `internal/ports/geoip.go`。
- `validator` 不再依赖 `fetcher` 的 IP 查询实现。
- `main.go` 和 WebUI 显式注入 GeoIP resolver。

### 第 4 阶段：代理池策略抽取

提交：`9ed816c 抽取代理池策略`

- 新增 `pool/policy.go`。
- 抽出池状态判断、slot 决策、抓取判断和替换判断。
- `pool.Manager` 保持对外接口稳定。

### 第 5 阶段：代理选择和失败处理抽取

提交：`8e4b198 抽取代理选择和失败上报`

- 新增 `proxy.Selector`。
- 新增 `proxy.FailureReporter`。
- HTTP 与 SOCKS5 server 共用选择与失败处理逻辑。
- 增加选择和失败策略测试。

### 基础阶段：代理源和解析增强

提交：`67d6486 补充代理源并增强解析`

- 增加多个公开代理源。
- 增强 `fetcher.parseProxyList`。
- 新增 fetcher 解析测试。

## 当前结构问题

优先级从高到低：

1. `storage.Storage` 职责过多：代理、订阅、源状态、统计、迁移都在一个对象里。
2. `webui` handler 直接调用多个底层模块，业务逻辑分散。
3. `custom.Manager` 同时处理订阅、sing-box 进程、验证和存储。
4. `proxy` 内仍有上游拨号、HTTP CONNECT 响应判断等可拆逻辑。
5. `main.go` 仍承担较多后台任务调度细节。

## 下一阶段建议

### 第 6 阶段：拆 storage 仓储边界

目标：先不改变数据库 schema，只拆接口和文件。

建议拆分：

```text
storage/
  storage.go              # DB 打开、迁移、事务基础
  proxy_repository.go     # proxies 表读写
  subscription_repository.go
  source_repository.go
  stats_repository.go
```

配套 ports：

```text
internal/ports/proxy_store.go
internal/ports/subscription_store.go
internal/ports/source_store.go
```

验收：

- 外部行为不变。
- `go test -count=1 ./...` 通过。
- `storage` 文件职责更清晰。

### 第 7 阶段：WebUI service 化

目标：WebUI handler 只负责 HTTP 输入输出。

建议新增：

```text
internal/service/admin_service.go
internal/service/subscription_service.go
internal/service/config_service.go
```

把这些逻辑移出 handler：

- 手动添加/删除代理。
- 手动验证代理。
- 更新配置。
- 触发抓取和健康检查。
- 订阅 CRUD 和刷新。

### 第 8 阶段：订阅管理解耦

目标：把 `custom.Manager` 拆成小组件。

建议：

```text
custom/parser.go
custom/fetcher.go
custom/converter.go
custom/singbox_process.go
internal/service/subscription_refresh_service.go
```

### 第 9 阶段：上游拨号器抽象

目标：让 `proxy.Server` 只负责协议入口。

建议：

```text
proxy/upstream_dialer.go
proxy/http_transport.go
proxy/socks5_transport.go
```

抽出：

- HTTP CONNECT 到上游代理。
- SOCKS5 handshake 到上游代理。
- HTTP client 构建。
- CONNECT 响应解析。

## 每阶段固定流程

```powershell
gofmt -w <changed-files>
go test -count=1 ./...
git diff --check
git status --short --branch
git commit -m "..."
git push
```

## 当前分支

```text
refactor/decouple-core
```

远端：

```text
https://github.com/y08lin4/GoProxy.git
```
