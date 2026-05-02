# GoProxy 重构计划

本文描述当前仓库的重构现状与后续建议，基于当前 `main` / `refactor/decouple-core` 代码状态维护。

## 目标结构

目标是把项目稳定收敛到下面的分层：

```text
main
  装配依赖、进程生命周期、信号处理

domain
  核心模型

ports
  存储 / GeoIP / 运行时能力边界

service
  补池、代理管理、订阅管理、源管理等应用流程

adapter
  fetcher / validator / storage / proxy / webui / custom
```

## 已完成的关键阶段

### 1. 补池流程服务化

- 抽出 `internal/service/refill_service.go`
- `main.go` 不再手写补池流程细节
- 验证流程支持 `context.Context` 取消

### 2. 领域模型与 ports 抽取

- 抽出 `internal/domain`
- 抽出 `internal/ports`
- fetcher / pool / validator / proxy 等开始依赖接口或领域模型

### 3. GeoIP 解耦

- 抽出 `internal/geoip/resolver.go`
- validator 不再直接依赖旧的 GeoIP 实现位置

### 4. 池策略与代理选择解耦

- 抽出 `pool.Policy`
- 抽出 `proxy.Selector`
- 抽出 `proxy.FailureReporter`
- 上游代理拨号逻辑集中到 `proxy/upstream_dialer.go`

### 5. storage 边界拆分第一轮

已抽出的接口包括：

```text
ProxyPoolStore
HealthCheckStore
ProxyRuntimeStore
SubscriptionStore
SubscriptionRuntime
ProxyAdminStore
```

当前生产代码中，只有 `main` 仍直接依赖 `storage` 具体实现。

### 6. WebUI service 化第一轮

已新增：

```text
ProxyAdminService
SubscriptionAdminService
SourceAdminService
```

当前 WebUI 的代理管理、订阅管理、源状态展示已大幅下沉到 service 层。

### 7. 生命周期统一

已完成：

- 健康检查器支持 `context` 停止
- 优化器支持 `context` 停止
- 自定义订阅管理器支持 `context` 停止
- WebUI / HTTP / SOCKS5 服务支持 `Run(ctx)` 优雅退出
- `main` 使用统一信号驱动的生命周期控制

### 8. 代理源配置化与状态可视化

- 配置支持 `extra_sources` / `disabled_source_urls`
- fetcher 根据运行时配置动态构建 fast / slow 源集合
- 源状态支持成功率、健康分、禁用状态展示
- WebUI 新增源状态面板

### 9. 代理分页与订阅刷新状态

- 代理列表 API 支持分页、协议过滤、国家过滤
- WebUI 代理表接入分页器
- 订阅刷新任务增加 `running / validating / success / failed` 状态快照

### 10. validator 策略链

validator 已拆分为：

- 连通性探测
- 状态码检查
- 响应时间限制
- 出口信息解析
- 地理过滤
- HTTP CONNECT 检查

当前实现保留原有外部接口，但内部已改成可组合策略链。

### 11. 配置读取收口

当前已经引入：

```text
config.Provider
config.GlobalProvider
config.StaticProvider
```

`main` 作为装配层显式创建 `StaticProvider` 并注入到多个模块。

### 12. 测试补强

当前已补充：

- `ProxyAdminService` 单元测试
- `SubscriptionAdminService` 单元测试
- `SourceAdminService` 单元测试
- WebUI API 集成测试骨架
- 订阅流程 API 集成测试
- 源配置 / 源状态联动测试

## 当前状态总结

目前项目已经具备这些特征：

- `storage` 仅被 `main` 直接依赖
- `webui` 不再直接依赖 `storage`
- 代理管理、订阅管理、源状态管理已有独立应用服务
- 后台任务具备统一的停止路径
- validator 内部逻辑已模块化
- 核心功能保持兼容

## 剩余建议

### A. 继续减少服务层内部对配置快照的散布

虽然全局 `config.Get()` 已大幅收口，但模块内部仍有不少 `provider.Get()` 调用。

后续可以继续做：

- 把同一次流程需要的配置统一收成局部快照
- 避免同一流程中反复读取 provider

### B. 补更强的端到端测试

后续建议继续补：

- 启动 `httptest.Server` 的更完整 WebUI API 路径
- 自定义订阅导入到代理池的端到端测试
- 配置保存与后台任务联动测试

### C. 继续收口文档与中文文案

后续适合继续做：

- 继续清理历史乱码文本
- 统一 ports / service / adapter 命名用词
- 保持 README / REFACTOR_PLAN / CHANGELOG 与实现同步

## 推荐日常验证流程

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
