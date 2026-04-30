# Windows 一键启动说明

## 使用方式

双击仓库根目录的：

```text
start-windows.bat
```

脚本会自动完成：

1. 读取 Windows 系统代理，并设置为本次启动的 `HTTP_PROXY` / `HTTPS_PROXY`
2. 检查本机是否已安装 Go
3. 执行 `go mod download` 补全依赖库
4. 编译 `.bin/proxygo.exe`
5. 启动 GoProxy WebUI
6. 自动打开浏览器访问 `http://localhost:7778`

## 默认入口

- WebUI：`http://localhost:7778`
- 默认密码：`goproxy`
- HTTP 随机代理：`127.0.0.1:7777`
- HTTP 最低延迟代理：`127.0.0.1:7776`
- SOCKS5 随机代理：`127.0.0.1:7779`
- SOCKS5 最低延迟代理：`127.0.0.1:7780`

## 修改 WebUI 密码

PowerShell 中设置环境变量后再启动：

```powershell
$env:WEBUI_PASSWORD="your_strong_password"
.\start-windows.ps1
```

## 依赖说明

项目已切换为纯 Go SQLite 驱动 `modernc.org/sqlite`，Windows 本地启动不再需要额外安装 GCC / MinGW / CGO 工具链。

如果依赖下载失败，先确认 Windows 系统代理可访问 GitHub / Go Module Proxy，或在当前 PowerShell 手动设置：

```powershell
$env:HTTP_PROXY="http://127.0.0.1:2080"
$env:HTTPS_PROXY="http://127.0.0.1:2080"
.\start-windows.ps1
```
