# GoProxy 测试脚本

本目录包含用于测试 GoProxy 代理端口的脚本。脚本通常会持续请求目标站点，按 `Ctrl+C` 停止并输出统计。

## 前置条件

先启动 GoProxy：

```bash
go run .
```

或 Windows：

```powershell
.\start-windows.ps1
```

等待代理池有可用代理后再测试。

## 端口

| 端口 | 协议 | 策略 |
| --- | --- | --- |
| `7777` | HTTP/HTTPS | 随机轮换 |
| `7776` | HTTP/HTTPS | 最低延迟 |
| `7779` | SOCKS5 | 随机轮换 |
| `7780` | SOCKS5 | 最低延迟 |

## curl 快速测试

```bash
curl -x http://127.0.0.1:7777 https://api.ipify.org
curl -x http://127.0.0.1:7776 https://api.ipify.org
curl --socks5 127.0.0.1:7779 https://api.ipify.org
curl --socks5 127.0.0.1:7780 https://api.ipify.org
```

## Bash 脚本

```bash
./test_proxy.sh 7777
./test_proxy.sh 7776
./test_socks5.sh 7779
./test_socks5.sh 7780
```

## Go / Python 脚本

如果目录内存在 Go 或 Python 测试脚本，可以直接运行对应文件：

```bash
go run test_proxy.go
python test_proxy.py
```

SOCKS5 Python 测试可能需要：

```bash
pip install requests[socks]
```

## 判断结果

关注：

- 请求成功率。
- 出口 IP 是否变化。
- 延迟是否稳定。
- 是否出现连续超时或 `no available proxy`。

随机端口预期出口 IP 更分散；最低延迟端口预期更稳定但 IP 变化较少。
