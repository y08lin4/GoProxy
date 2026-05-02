# 更新日志

本文记录当前仓库的重要变更。历史发布说明不再作为本文档主体，后续版本以本仓库提交和标签为准。

## [Unreleased]

### 新增

- 从 Proxyfind 目录筛选并验证更多纯文本代理源，扩充 OpenProxyList、ProxySpace、Jetkai、ShiftyTR、ErcinDedeoglu 等来源。
- 新增 `internal/ports.ProxyPoolStore`，为代理池管理建立存储接口边界。

### 变更

- `fetcher` 和 `validator` 的公开输入输出改为使用 `internal/domain` 类型，减少对 `storage` 具体实现的依赖。
- `pool.Manager` 改为依赖代理池存储接口，不再直接依赖 `*storage.Storage`。
- `HealthChecker` 改为依赖健康检查存储接口，并移除未使用的旧版 `checker.Checker` 实现。
- `proxy` 运行时改为依赖选择和使用上报存储接口，HTTP/SOCKS5 入口不再直接依赖 `storage` 包。
- 移除订阅解析默认调试落盘，WebUI 改用仓储方法标记贡献订阅，并为单个代理刷新增加按地址查询。
- `optimizer` 改为使用领域模型候选，不再直接依赖 `storage` 包。
- 新增订阅管理和 WebUI 代理管理端口接口，`custom.Manager` 不再直接依赖 `storage` 包。
- 新增 `ProxyAdminService` 与配置 Provider，WebUI 的代理统计、列表、删除和刷新逻辑开始下沉到 service 层。

### 重构

- 计划继续拆分 `storage` 仓储边界。
- 计划将 WebUI handler 的业务逻辑下沉到 service 层。
- 计划抽象上游拨号器，进一步简化 `proxy` 协议入口。

## [refactor/decouple-core] - 2026-05-02

### 新增

- 新增 `internal/domain`，集中核心领域模型。
- 新增 `internal/ports`，定义补池和 GeoIP 边界接口。
- 新增 `internal/service/RefillService`，统一补池流程。
- 新增 `internal/geoip/Resolver`，独立 IP 信息解析。
- 新增 `pool.Policy`，封装代理池状态、slot 和替换策略。
- 新增 `proxy.Selector`，统一 HTTP/SOCKS5 的代理选择逻辑。
- 新增 `proxy.FailureReporter`，统一成功/失败记录和下线策略。

### 变更

- `main.go` 从直接执行补池逻辑改为调用 `RefillService`。
- `validator` 改为通过接口使用 GeoIP resolver。
- `pool.Manager` 通过 policy 执行策略判断，对外接口保持兼容。
- HTTP 与 SOCKS5 代理服务复用相同选择逻辑和失败处理逻辑。
- 公开代理源列表扩充，代理列表解析更宽容。

### 测试

- 新增 fetcher 解析测试。
- 新增 pool policy 测试。
- 新增 proxy selector / failure reporter 测试。
- 当前验证命令：`go test -count=1 ./...`。

### 相关提交

```text
8e4b198 抽取代理选择和失败上报
9ed816c 抽取代理池策略
81ff534 拆分 GeoIP 解析与验证器
b78512a 抽取核心领域模型和补池端口
849ce48 重构补池流程并增加验证取消
67d6486 补充代理源并增强解析
```

## 基线功能

当前项目保留并维护以下功能：

- HTTP/HTTPS CONNECT 代理入口。
- SOCKS5 代理入口。
- WebUI 管理面板。
- 公开代理源抓取和验证。
- Clash/V2Ray 订阅导入。
- sing-box 协议转换。
- 国家黑名单/白名单过滤。
- SQLite 持久化。
- Docker 和 Windows 本地运行支持。

## 仓库

- 当前仓库：<https://github.com/y08lin4/GoProxy>
- 当前重构分支：`refactor/decouple-core`
