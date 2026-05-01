# GoProxy 架构与代理池设计

本文记录当前仓库的运行架构和代理池策略，用于后续重构时对齐边界。

## 设计目标

- 单二进制运行，部署简单。
- 免费代理池自动补充，订阅代理按需导入。
- HTTP 与 SOCKS5 双协议入口统一对外服务。
- 验证、入池、选择、失败处理逐步解耦，便于测试和替换实现。

## 运行链路

```text
公开代理源 ─┐
           ├─ fetcher ─ validator ─ pool.Manager ─ storage(SQLite)
订阅源 ─────┘      │           │             │
                   │           │             ├─ pool.Policy
                   │           └─ geoip.Resolver
                   └─ SourceManager

HTTP/SOCKS5 请求 ─ proxy.Server / SOCKS5Server
                 ├─ Selector：按模式、协议、延迟/随机策略选代理
                 └─ FailureReporter：记录成功/失败，删除或禁用代理
```

## 模块边界

| 模块 | 职责 |
| --- | --- |
| `main.go` | 只做依赖装配、启动端口和后台 goroutine。 |
| `config` | 默认配置、环境变量、`config.json` 持久化。 |
| `fetcher` | 代理源抓取、解析、源状态熔断。 |
| `validator` | HTTP/SOCKS5 连通性验证、HTTPS CONNECT 验证、出口 IP 检测。 |
| `internal/geoip` | IP 地理位置和画像查询。 |
| `internal/service` | 应用层流程，例如补池 `RefillService`。 |
| `pool` | 池容量、协议比例、状态和替换策略。 |
| `proxy` | HTTP/SOCKS5 协议入口、代理选择、失败上报。 |
| `custom` | 订阅解析、刷新、sing-box 转换。 |
| `storage` | SQLite 数据访问，后续需要继续拆分。 |
| `webui` | 管理页面和 API，后续应收敛为调用 service。 |

## 数据模型

### `proxies`

核心字段：

| 字段 | 说明 |
| --- | --- |
| `address` | 上游地址，格式 `host:port`。 |
| `protocol` | `http` 或 `socks5`。 |
| `source` | `free` 或 `custom`。 |
| `status` | `active` / `degraded` / `disabled`。 |
| `latency` | 验证延迟，毫秒。 |
| `quality_grade` | `S` / `A` / `B` / `C`。 |
| `use_count` / `success_count` / `fail_count` | 使用统计。 |
| `exit_ip` / `country_code` / `city` | 出口与地理信息。 |
| `subscription_id` | 订阅代理关联 ID。 |

### `source_status`

记录公开代理源的成功、失败、连续失败和冷却状态，用于熔断质量差的源。

### `subscriptions`

记录订阅名称、URL/文件、格式、刷新间隔、最后抓取时间和节点数量。

## 补池流程

`RefillService` 负责一次完整补池：

1. 查询池状态和缺口。
2. 从可用源抓取候选代理。
3. 按配置限制每个源保留的候选数量。
4. 并发验证候选代理。
5. 根据 `pool.Policy` 决定新增或替换。
6. 写入 SQLite。

并发补池通过 service 内部锁保护，避免多个触发器同时填池。

## 池策略

`pool.Policy` 目前封装：

- 池状态判断：healthy / warning / critical / emergency。
- 是否需要抓取。
- HTTP/SOCKS5 slot 分配。
- 新代理是否足以替换旧代理。
- 不同健康状态下的延迟阈值。

默认池容量来自配置：

```text
PoolMaxSize        = 100
PoolHTTPRatio      = 0.3
PoolMinPerProtocol = 10
ReplaceThreshold   = 0.7
```

## 代理选择策略

`proxy.Selector` 统一 HTTP 与 SOCKS5 入口的选择逻辑：

- `mixed`：免费和订阅混合。
- `mixed + CustomPriority`：订阅优先，失败后 fallback 到全部。
- `mixed + CustomFreePriority`：免费优先，失败后 fallback 到全部。
- `custom_only`：只使用订阅代理。
- `free_only`：只使用免费代理。
- HTTP 入口可以选择 HTTP 或 SOCKS5 上游。
- SOCKS5 入口只选择 SOCKS5 上游。
- 支持随机与最低延迟两种策略。

## 失败处理策略

`proxy.FailureReporter` 统一成功/失败记录：

- 成功：增加 `use_count` 和 `success_count`。
- 失败：增加 `use_count` 和 `fail_count`。
- 免费代理失败：从池中删除。
- 订阅代理失败：置为 `disabled`，等待订阅刷新或探测唤醒。

## 订阅代理生命周期

```text
添加订阅 → 拉取/解析 → sing-box 转本地 SOCKS5 → 验证 → 入 custom 池
       ↑                                                   │
       └──────────── 定时刷新 / 手动刷新 / 探测唤醒 ───────┘
```

加密协议节点不直接作为上游使用，而是通过 sing-box 暴露成本地 SOCKS5 地址，再进入统一验证和选择流程。

## GeoIP 与地理过滤

`validator` 只负责验证流程，具体 IP 信息解析由 `internal/geoip.Resolver` 提供。过滤顺序：

1. 校验出口 IP。
2. 查询国家代码和地理信息。
3. 若 `ALLOWED_COUNTRIES` 非空，只允许白名单国家。
4. 否则按 `BLOCKED_COUNTRIES` 黑名单过滤。
5. 通过后再入池。

## 后台任务

| 任务 | 说明 |
| --- | --- |
| 补池 ticker | 定期触发 `RefillService`。 |
| 健康检查 | 批量复检池内代理。 |
| 优化器 | 用更快的新代理替换较差旧代理。 |
| 订阅管理器 | 刷新订阅、维护 sing-box 进程、唤醒禁用节点。 |

## 后续解耦方向

- 将 `storage.Storage` 拆成代理仓储、订阅仓储、源状态仓储。
- 为 WebUI 增加 service 层，减少 handler 直接访问 storage/pool/custom。
- 将 HTTP CONNECT 和 SOCKS5 dialer 抽成独立上游拨号器。
- 将运行时调度从 `main.go` 继续下沉到 application 层。
