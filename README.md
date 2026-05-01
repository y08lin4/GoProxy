# GoProxy

GoProxy 是一个基于 Go 的代理池服务。它从公开代理源和用户订阅中获取 HTTP/SOCKS5 节点，经过出口 IP、地理位置、延迟和可用性验证后入池，再对外提供 HTTP 与 SOCKS5 代理端口。

当前仓库：<https://github.com/y08lin4/GoProxy>

## 当前定位

- **自维护项目**：文档、分支和后续开发均以本仓库为准，不再沿用外部项目说明。
- **渐进式解耦中**：核心领域模型、补池服务、GeoIP、池策略、代理选择和失败上报已拆出。
- **本地优先**：推荐从源码构建或本地 Docker 镜像运行；如果要发布镜像，请使用本仓库自己的镜像命名空间。

## 功能概览

### 代理池

- 自动抓取公开 HTTP/SOCKS5 代理源。
- 支持 Clash/V2Ray 订阅导入。
- 使用 sing-box 将 vmess、vless、trojan、shadowsocks、hysteria2、anytls 等节点转换为本地 SOCKS5。
- 支持免费代理与订阅代理混用、订阅优先、免费优先、仅订阅、仅免费等模式。

### 验证与质量控制

- 验证代理连通性、出口 IP、地理位置和延迟。
- 支持国家黑名单/白名单过滤。
- 按质量等级和延迟选择代理。
- 失败代理自动记录、删除或禁用。
- 免费池按容量、协议比例和替换策略自动补充。

### 对外服务

| 端口 | 协议 | 策略 | 说明 |
| --- | --- | --- | --- |
| `7776` | HTTP/HTTPS CONNECT | 最低延迟 | 稳定优先 |
| `7777` | HTTP/HTTPS CONNECT | 随机轮换 | IP 多样性优先 |
| `7778` | HTTP | WebUI | 管理面板 |
| `7779` | SOCKS5 | 随机轮换 | SOCKS5 入口 |
| `7780` | SOCKS5 | 最低延迟 | 稳定 SOCKS5 入口 |

## 快速开始

### Windows 一键启动

```powershell
.\start-windows.ps1
```

脚本会读取 Windows 系统代理，下载 Go 依赖，编译到 `.bin/proxygo.exe`，启动服务并打开 WebUI。

### 本地源码运行

```powershell
go mod download
go run .
```

或编译后运行：

```powershell
go build -o .bin/proxygo.exe .
.\.bin\proxygo.exe
```

Linux/macOS：

```bash
go mod download
go build -o proxygo .
./proxygo
```

### Docker 本地构建

```bash
docker build -t goproxy:local .
docker run -d --name goproxy \
  -p 7776:7776 -p 7777:7777 -p 7778:7778 -p 7779:7779 -p 7780:7780 \
  -e DATA_DIR=/app/data \
  -e WEBUI_PASSWORD=goproxy \
  -v goproxy-data:/app/data \
  goproxy:local
```

如果使用 `docker-compose.yml`，请确认镜像名已经改为本仓库发布的镜像，或启用本地 `build: .`，避免拉取无关镜像。

## 使用代理

### HTTP/HTTPS

```bash
curl -x http://127.0.0.1:7777 https://api.ipify.org
curl -x http://127.0.0.1:7776 https://api.ipify.org
```

### SOCKS5

```bash
curl --socks5 127.0.0.1:7779 https://api.ipify.org
curl --socks5 127.0.0.1:7780 https://api.ipify.org
```

### 启用代理认证

```bash
PROXY_AUTH_ENABLED=true
PROXY_AUTH_USERNAME=proxy
PROXY_AUTH_PASSWORD=change-me
```

使用示例：

```bash
curl -x http://proxy:change-me@127.0.0.1:7777 https://api.ipify.org
curl --socks5 proxy:change-me@127.0.0.1:7779 https://api.ipify.org
```

## WebUI

访问：

```text
http://127.0.0.1:7778
```

默认密码：

```text
goproxy
```

生产或公网环境务必修改：

```bash
WEBUI_PASSWORD=your-strong-password
```

## 订阅导入

WebUI 中可以添加订阅：

1. 进入 WebUI。
2. 打开订阅管理。
3. 添加订阅 URL 或本地订阅文件。
4. 选择格式并保存。
5. 等待验证和转换完成。

本地运行加密协议订阅时需要安装 `sing-box`，或者通过 `SINGBOX_PATH` 指向可执行文件。Docker 镜像构建时会内置 sing-box。

## 常用环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DATA_DIR` | 空 | 运行时数据目录；Docker 推荐 `/app/data` |
| `WEBUI_PASSWORD` | `goproxy` | WebUI 密码 |
| `PROXY_AUTH_ENABLED` | `false` | 是否启用代理端口认证 |
| `PROXY_AUTH_USERNAME` | `proxy` | 代理认证用户名 |
| `PROXY_AUTH_PASSWORD` | 空 | 代理认证密码 |
| `BLOCKED_COUNTRIES` | `CN` | 国家黑名单，逗号分隔 |
| `ALLOWED_COUNTRIES` | 空 | 国家白名单；非空时优先于黑名单 |
| `CUSTOM_PROXY_MODE` | `mixed` | `mixed` / `custom_only` / `free_only` |
| `SINGBOX_PATH` | `sing-box` | sing-box 可执行文件路径 |
| `TZ` | `Asia/Shanghai` | 时区 |

更多数据目录说明见 `DATA_DIRECTORY.md`，地理过滤说明见 `GEO_FILTER.md`。

## 项目结构

```text
main.go                  # 装配依赖、启动服务和后台任务
config/                  # 配置加载和持久化
fetcher/                 # 公开代理源抓取、解析、源状态管理
validator/               # 代理连通性、出口 IP、HTTPS 能力验证
internal/domain/         # 核心领域模型
internal/ports/          # 服务边界接口
internal/geoip/          # GeoIP/IP 信息解析
internal/service/        # 补池服务等应用层流程
pool/                    # 代理池状态、容量和替换策略
proxy/                   # HTTP/SOCKS5 入口、代理选择、失败上报
custom/                  # 订阅导入和 sing-box 进程管理
storage/                 # SQLite 持久化
webui/                   # 管理页面和 API
checker/                 # 后台健康检查
optimizer/               # 后台优化替换
test/                    # 代理连通性测试脚本
```

## 当前重构进度

已完成：

- 补充代理源并增强解析。
- 抽出 `RefillService`。
- 抽出 `internal/domain` 和 `internal/ports`。
- 将 GeoIP 解析从 `validator` 解耦到 `internal/geoip`。
- 抽出 `pool.Policy`。
- 抽出 `proxy.Selector` 和 `proxy.FailureReporter`。

下一步重点：

- 拆分 `storage` 巨型对象。
- 让 WebUI handler 只调用 service。
- 继续收敛订阅管理与代理池之间的边界。

## 测试

```bash
go test -count=1 ./...
```

代理服务启动后，可以使用 `test/` 目录下脚本做端口连通性测试。

## 文档索引

- `REFACTOR_PLAN.md`：解耦重构路线。
- `POOL_DESIGN.md`：代理池和模块设计。
- `DATA_DIRECTORY.md`：数据目录、备份和恢复。
- `GEO_FILTER.md`：国家过滤配置。
- `WINDOWS_START.md`：Windows 一键启动说明。
- `test/README.md`：测试脚本说明。

## 免责声明

本项目仅用于学习、研究和自建代理池管理。公开代理源质量不可控，不保证可用性、稳定性或安全性。请遵守当地法律法规，不要将本项目用于违法违规用途。
