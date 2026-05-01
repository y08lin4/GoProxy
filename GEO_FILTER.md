# 地理过滤配置指南

GoProxy 支持按出口国家过滤代理，适合控制代理池地区分布。过滤在代理验证和入池前执行。

## 两种模式

### 黑名单模式

屏蔽指定国家，其它国家允许入池。

```bash
BLOCKED_COUNTRIES=CN,RU,KP
ALLOWED_COUNTRIES=
```

默认配置为：

```bash
BLOCKED_COUNTRIES=CN
```

### 白名单模式

只允许指定国家入池。只要 `ALLOWED_COUNTRIES` 非空，就优先使用白名单并忽略黑名单。

```bash
ALLOWED_COUNTRIES=US,JP,KR,SG
```

## 本地运行配置

PowerShell：

```powershell
$env:BLOCKED_COUNTRIES = "CN,RU"
$env:ALLOWED_COUNTRIES = ""
go run .
```

Bash：

```bash
BLOCKED_COUNTRIES=CN,RU ALLOWED_COUNTRIES= go run .
```

## Docker 配置

```bash
docker run -d --name goproxy \
  -e DATA_DIR=/app/data \
  -e BLOCKED_COUNTRIES=CN,RU \
  -e ALLOWED_COUNTRIES= \
  -v goproxy-data:/app/data \
  -p 7776:7776 -p 7777:7777 -p 7778:7778 -p 7779:7779 -p 7780:7780 \
  goproxy:local
```

白名单示例：

```bash
docker run -d --name goproxy \
  -e DATA_DIR=/app/data \
  -e BLOCKED_COUNTRIES= \
  -e ALLOWED_COUNTRIES=US,JP,KR,SG \
  -v goproxy-data:/app/data \
  -p 7776:7776 -p 7777:7777 -p 7778:7778 -p 7779:7779 -p 7780:7780 \
  goproxy:local
```

## WebUI 配置

WebUI 设置保存到 `config.json`，优先级高于首次启动环境变量。修改后会影响后续验证和入池流程。

## 过滤流程

```text
候选代理
  ↓
连通性验证
  ↓
获取出口 IP
  ↓
GeoIP 查询国家代码
  ↓
白名单/黑名单判断
  ↓
通过后入池
```

规则：

1. `ALLOWED_COUNTRIES` 非空：只允许白名单国家。
2. `ALLOWED_COUNTRIES` 为空：屏蔽 `BLOCKED_COUNTRIES` 中的国家。
3. 两者都为空：不做国家过滤。
4. 国家代码统一按 ISO 3166-1 alpha-2 大写处理，例如 `US`、`JP`、`SG`。

## 常用国家代码

| 地区 | 代码 |
| --- | --- |
| 中国大陆 | `CN` |
| 中国香港 | `HK` |
| 中国台湾 | `TW` |
| 日本 | `JP` |
| 韩国 | `KR` |
| 新加坡 | `SG` |
| 美国 | `US` |
| 加拿大 | `CA` |
| 英国 | `GB` |
| 德国 | `DE` |
| 法国 | `FR` |
| 荷兰 | `NL` |
| 澳大利亚 | `AU` |

## 常见场景

### 屏蔽中国大陆

```bash
BLOCKED_COUNTRIES=CN
ALLOWED_COUNTRIES=
```

### 仅使用亚太节点

```bash
ALLOWED_COUNTRIES=JP,KR,SG,HK,TW,AU
```

### 仅使用欧美节点

```bash
ALLOWED_COUNTRIES=US,CA,GB,DE,FR,NL
```

### 不做地理限制

```bash
BLOCKED_COUNTRIES=
ALLOWED_COUNTRIES=
```

## 查看结果

```bash
sqlite3 data/proxy.db "SELECT country_code, COUNT(*) FROM proxies GROUP BY country_code ORDER BY COUNT(*) DESC;"
sqlite3 data/proxy.db "SELECT address, protocol, country_code, latency FROM proxies WHERE country_code='US' LIMIT 20;"
```

## 注意事项

- GeoIP 服务不可用时，代理可能无法获得完整国家信息，验证结果会受影响。
- 白名单过窄会明显降低入池数量。
- 已入池代理在配置修改后不会自动全部删除，建议触发健康检查或手动清理不符合规则的代理。
