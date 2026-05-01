# 数据目录说明

GoProxy 的运行时数据由 `DATA_DIR` 决定。未设置时，数据会落在当前工作目录；Docker 环境建议设置为 `/app/data` 并挂载持久化卷。

## 数据内容

| 文件/目录 | 说明 |
| --- | --- |
| `proxy.db` | SQLite 主数据库。 |
| `proxy.db-wal` / `proxy.db-shm` | SQLite WAL 模式文件，运行中可能存在。 |
| `config.json` | WebUI 保存的运行配置。 |
| `singbox/` | sing-box 生成的本地转换配置和运行状态。 |
| `subscriptions/` | 本地订阅文件或缓存。 |

## 数据库表

| 表 | 说明 |
| --- | --- |
| `proxies` | 代理池、质量、出口 IP、国家、使用统计。 |
| `source_status` | 公开代理源的成功/失败和熔断状态。 |
| `subscriptions` | 订阅配置和刷新状态。 |

## 本地运行

推荐显式设置数据目录，避免把运行数据散落在源码根目录：

```powershell
$env:DATA_DIR = "$PWD\data"
go run .
```

Linux/macOS：

```bash
export DATA_DIR="$PWD/data"
go run .
```

## Docker 运行

本仓库推荐本地构建镜像：

```bash
docker build -t goproxy:local .
docker run -d --name goproxy \
  -e DATA_DIR=/app/data \
  -v goproxy-data:/app/data \
  -p 7776:7776 -p 7777:7777 -p 7778:7778 -p 7779:7779 -p 7780:7780 \
  goproxy:local
```

数据卷位置由 Docker 管理：

```bash
docker volume inspect goproxy-data
```

## 查看数据库

```bash
sqlite3 data/proxy.db ".tables"
sqlite3 data/proxy.db "SELECT address, protocol, source, status, latency, country_code FROM proxies LIMIT 20;"
sqlite3 data/proxy.db "SELECT url, status, consecutive_fails, last_success FROM source_status;"
```

Docker 卷中查看：

```bash
docker run --rm -it -v goproxy-data:/data alpine sh
```

如需在容器内使用 `sqlite3`，可使用带 sqlite 工具的镜像或在宿主机导出后查看。

## 备份

### 备份整个数据目录

```bash
tar -czf goproxy-backup-$(date +%Y%m%d).tar.gz data/
```

### 备份 Docker 卷

```bash
docker run --rm \
  -v goproxy-data:/data \
  -v "$PWD":/backup \
  alpine tar czf /backup/goproxy-backup-$(date +%Y%m%d).tar.gz -C /data .
```

## 恢复

```bash
docker stop goproxy

docker run --rm \
  -v goproxy-data:/data \
  -v "$PWD":/backup \
  alpine sh -c "cd /data && tar xzf /backup/goproxy-backup-20260502.tar.gz"

docker start goproxy
```

## 清理

### 仅清空代理池

```bash
sqlite3 data/proxy.db "DELETE FROM proxies;"
```

### 完全重置本地数据

```bash
rm -rf data/
```

### 完全重置 Docker 数据

```bash
docker rm -f goproxy
docker volume rm goproxy-data
```

## 注意事项

- `proxy.db-wal` 和 `proxy.db-shm` 是 SQLite 正常运行文件，不要在进程运行时单独删除。
- 生产环境务必备份 `proxy.db` 和 `config.json`。
- 订阅代理依赖 sing-box，本地数据迁移时也建议保留 `singbox/` 相关状态。
