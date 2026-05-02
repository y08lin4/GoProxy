# GoProxy

GoProxy 是一个基于 Go 的代理池服务。

它会从公开代理源和用户订阅中获取 HTTP / SOCKS5 节点，经过连通性、出口信息、地理位置、延迟和协议能力验证后入池，再对外提供 HTTP 与 SOCKS5 代理端口。

仓库地址：<https://github.com/y08lin4/GoProxy>

## 当前状态

当前仓库已经完成一轮较大规模解耦，核心结构从“handler 直连 storage + main 手工编排全部流程”演进为：

```text
main
  只负责装配具体实现和进程生命周期

internal/domain
  核心领域模型

internal/ports
  存储 / GeoIP / 运行时边界接口

internal/service
  补池、代理管理、订阅管理、源状态聚合等应用服务

adapter
  fetcher / validator / storage / proxy / webui / custom
```

当前生产代码中，`storage` 只被 `main` 直接依赖；其余模块均通过 `ports` / `service` 间接访问。

## 功能概览

### 代理池

- 抓取公开 HTTP / SOCKS5 代理源
- 支持导入 Clash / V2Ray / 纯文本订阅
- 支持通过 `sing-box` 把 `vmess`、`vless`、`trojan`、`ss`、`hysteria2` 等节点转换为本地 SOCKS5
- 支持免费代理、订阅代理、混用优先级模式

### 验证与筛选

- 连通性检测
- 出口 IP / 地理位置解析
- 国家黑白名单过滤
- 延迟与质量等级筛选
- HTTP 代理额外做 HTTPS CONNECT 能力检查
- 验证逻辑已拆成可组合策略链

### WebUI

- 代理列表分页、协议过滤、国家过滤
- 订阅管理、订阅刷新状态展示
- 代理源状态展示（成功率、健康分、启用状态）
- 配置在线保存

## 对外端口

| 端口 | 协议 | 说明 |
| --- | --- | --- |
| `7776` | HTTP/HTTPS CONNECT | 稳定代理（最低延迟） |
| `7777` | HTTP/HTTPS CONNECT | 随机轮换代理 |
| `7778` | HTTP | WebUI |
| `7779` | SOCKS5 | 随机轮换 SOCKS5 |
| `7780` | SOCKS5 | 稳定 SOCKS5 |

## 快速启动

### 本地运行

```powershell
go mod download
go run .
```

或编译后运行：

```powershell
go build -o .bin/proxygo.exe .
.\.bin\proxygo.exe
```

Linux / macOS：

```bash
go mod download
go build -o proxygo .
./proxygo
```

### Docker

```bash
docker build -t goproxy:local .
docker run -d --name goproxy \
  -p 7776:7776 -p 7777:7777 -p 7778:7778 -p 7779:7779 -p 7780:7780 \
  -e DATA_DIR=/app/data \
  -e WEBUI_PASSWORD=goproxy \
  -v goproxy-data:/app/data \
  goproxy:local
```

## 常用环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DATA_DIR` | 空 | 运行时数据目录 |
| `WEBUI_PASSWORD` | `goproxy` | WebUI 密码 |
| `PROXY_AUTH_ENABLED` | `false` | 是否开启代理认证 |
| `PROXY_AUTH_USERNAME` | `proxy` | 代理认证用户名 |
| `PROXY_AUTH_PASSWORD` | 空 | 代理认证密码 |
| `BLOCKED_COUNTRIES` | `CN` | 国家黑名单 |
| `ALLOWED_COUNTRIES` | 空 | 国家白名单，优先于黑名单 |
| `CUSTOM_PROXY_MODE` | `mixed` | `mixed` / `custom_only` / `free_only` |
| `SINGBOX_PATH` | `sing-box` | `sing-box` 可执行文件路径 |
| `TZ` | `Asia/Shanghai` | 时区 |

## 源配置

当前支持通过配置文件动态管理额外源和禁用源。

配置字段：

```json
{
  "extra_sources": [
    {"group": "slow", "protocol": "http", "url": "https://example.com/http.txt"},
    {"group": "fast", "protocol": "socks5", "url": "https://example.com/socks5.txt"}
  ],
  "disabled_source_urls": [
    "https://example.com/http.txt"
  ]
}
```

说明：

- `group`：`fast` 或 `slow`
- `protocol`：`http` / `https` / `socks4` / `socks5`
- `https` 会归一化为 `http`
- `socks4` 会按现有行为归一化为 `socks5`
- `disabled_source_urls` 可临时停用内置源或额外源

## 订阅刷新状态

WebUI 会显示订阅刷新任务的最新状态，当前包含：

- `running`
- `validating`
- `success`
- `failed`

批量刷新与单订阅刷新都会暴露状态快照，便于观察当前导入过程。

## 项目结构

```text
main.go
config/                  # 配置加载、Provider、持久化
fetcher/                 # 公开源抓取、源配置、源健康状态
validator/               # 连通性探测与验证策略链
internal/domain/         # 领域模型
internal/ports/          # 存储 / GeoIP / 运行时边界接口
internal/geoip/          # GeoIP / 出口信息解析
internal/service/        # 应用服务
pool/                    # 代理池状态与替换策略
proxy/                   # HTTP / SOCKS5 入口
custom/                  # 订阅导入与 sing-box 管理
storage/                 # SQLite 持久化
webui/                   # WebUI 与 API
checker/                 # 健康检查后台任务
optimizer/               # 优化轮换后台任务
```

## 测试

全量测试：

```bash
go test -count=1 ./...
```

当前已经补齐的重点测试包括：

- fetcher 解析与源配置测试
- pool 策略测试
- proxy selector / failure reporter 测试
- validator 策略链测试
- `ProxyAdminService` / `SubscriptionAdminService` / `SourceAdminService` 单元测试
- WebUI session / 配置 / 订阅 / 源状态相关测试

## 相关文档

- `REFACTOR_PLAN.md`：当前重构进度与后续路线
- `POOL_DESIGN.md`：代理池设计
- `DATA_DIRECTORY.md`：数据目录说明
- `GEO_FILTER.md`：地理过滤说明
- `WINDOWS_START.md`：Windows 启动说明
- `test/README.md`：测试脚本说明

## 免责声明

本项目仅用于学习、研究和自建代理池管理。公开代理源质量不可控，不保证可用性、稳定性或安全性。请遵守当地法律法规，不要将本项目用于违法违规用途。
