# Windows 一键启动说明

本仓库提供 `start-windows.ps1` 和 `start-windows.bat`，用于在 Windows 本地快速启动 GoProxy。

## 使用方式

PowerShell：

```powershell
.\start-windows.ps1
```

或双击：

```text
start-windows.bat
```

脚本流程：

1. 检查 Go 环境。
2. 读取 Windows 系统代理并用于依赖下载。
3. 执行 `go mod download`。
4. 编译 `.bin/proxygo.exe`。
5. 启动 GoProxy。
6. 打开 WebUI：`http://localhost:7778`。

## 默认入口

| 服务 | 地址 |
| --- | --- |
| WebUI | `http://localhost:7778` |
| HTTP 随机 | `127.0.0.1:7777` |
| HTTP 最低延迟 | `127.0.0.1:7776` |
| SOCKS5 随机 | `127.0.0.1:7779` |
| SOCKS5 最低延迟 | `127.0.0.1:7780` |

默认 WebUI 密码：

```text
goproxy
```

## 修改密码

启动前设置环境变量：

```powershell
$env:WEBUI_PASSWORD = "your-strong-password"
.\start-windows.ps1
```

## 代理与依赖下载

如果依赖下载失败，确认 Windows 系统代理可访问 GitHub 和 Go Module Proxy。也可以手动设置：

```powershell
$env:HTTP_PROXY = "http://127.0.0.1:2080"
$env:HTTPS_PROXY = "http://127.0.0.1:2080"
go mod download
```

## 依赖说明

项目使用纯 Go SQLite 驱动 `modernc.org/sqlite`，Windows 本地启动不需要额外安装 GCC、MinGW 或 CGO 工具链。
