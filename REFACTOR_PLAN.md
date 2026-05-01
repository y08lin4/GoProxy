# GoProxy 解耦重构计划

## 当前状态

GoProxy 已经按 `fetcher`、`validator`、`pool`、`proxy`、`webui`、`storage` 等目录拆包，但核心业务仍然耦合在一起：

- `main.go` 同时负责启动、调度、抓取、验证、入池和监控。
- `storage.Storage` 是巨型对象，混合代理、订阅、来源状态、统计和迁移。
- `validator` 依赖 `fetcher.GetExitIPInfo`，验证和出口信息查询边界不清晰。
- `pool.Manager` 同时做策略判断和数据库写入。
- `proxy` 和 `webui` 直接操作底层 `storage`，后续替换存储或策略较难。

## 目标架构

长期目标是把系统拆成明确的几层：

```text
cmd/goproxy
  main.go

internal/app
  lifecycle.go
  scheduler.go

internal/domain
  proxy.go
  source.go
  pool.go
  subscription.go

internal/ports
  repository.go
  fetcher.go
  validator.go
  selector.go
  geoip.go

internal/service
  refill_service.go
  pool_service.go
  health_service.go
  optimize_service.go
  subscription_service.go
  proxy_select_service.go

internal/adapter
  storage/sqlite
  source/httpfetcher
  validator/httpvalidator
  geoip
  proxyserver/http
  proxyserver/socks5
  webui
  config/file
```

## 分阶段落地

### 第 0 阶段：保存当前功能

- 单独提交代理源补充和解析增强。
- 确保 `go test -count=1 ./...` 通过。

### 第 1 阶段：抽出补池服务

- 从 `main.go` 移出 `smartFetchAndFill`。
- 新增 `internal/service.RefillService`。
- `main.go` 只负责装配依赖和启动生命周期。
- `ValidateStream` 增加 `context.Context` 取消能力。
- 新增 `MaxCandidatesPerSource`，限制单个源进入验证队列的候选数量。

### 第 2 阶段：抽 domain 和 ports

- 将 `storage.Proxy`、`storage.IPInfo`、`storage.Subscription`、`fetcher.Source` 等模型迁移到 `internal/domain`。
- 用类型别名兼容旧代码，避免一次性大改。
- 定义 repository / validator / fetcher 接口。

### 第 3 阶段：拆 validator 和 geoip

- 将出口 IP、地理信息、IPPure 查询从 `validator` 移到独立 `geoip` resolver。
- `validator` 只负责代理连通性和协议能力验证。
- 国家过滤策略独立成 policy。

### 第 4 阶段：拆 pool 策略和执行

- `PoolPolicy` 只做容量、slot、替换决策。
- `PoolService` 负责入池、替换、禁用和统计更新。
- `storage` 降级为 repository 实现。

### 第 5 阶段：拆 proxy server

- HTTP/SOCKS5 server 只做协议处理和转发。
- 代理选择交给 `ProxySelector`。
- 失败处理交给 `FailureReporter` / `PoolService`。

### 第 6 阶段：WebUI 只调用 service

- WebUI handler 不再直接操作 `storage`。
- API 按功能拆分 handler、middleware、DTO。
- 配置保存、订阅刷新、手动抓取都通过 service。

## 当前优先级

第一批重构只做低风险、高收益改动：

1. 抽出 `RefillService`。
2. 给验证流加取消机制。
3. 限制每个代理源候选数量。

这三个改动能明显降低新增代理源后的 goroutine、网络和 CPU 压力，同时为后续模块解耦打基础。
