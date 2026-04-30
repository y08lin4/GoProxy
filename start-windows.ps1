$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

Set-Location -LiteralPath $PSScriptRoot

function Write-Step {
    param([string]$Message)
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Use-SystemProxyIfAvailable {
    if ($env:HTTP_PROXY -or $env:HTTPS_PROXY) {
        Write-Host "[proxy] HTTP_PROXY/HTTPS_PROXY already set, keep current environment proxy."
        return
    }

    try {
        $regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Internet Settings"
        $settings = Get-ItemProperty -Path $regPath
        if ($settings.ProxyEnable -ne 1 -or [string]::IsNullOrWhiteSpace($settings.ProxyServer)) {
            Write-Host "[proxy] Windows system proxy is not enabled."
            return
        }

        $proxy = [string]$settings.ProxyServer
        if ($proxy -match "https=([^;]+)") {
            $proxy = $Matches[1]
        } elseif ($proxy -match "http=([^;]+)") {
            $proxy = $Matches[1]
        } elseif ($proxy.Contains(";")) {
            $proxy = ($proxy -split ";")[0] -replace "^[^=]+=", ""
        }

        if ($proxy -notmatch "^[a-zA-Z][a-zA-Z0-9+.-]*://") {
            $proxy = "http://$proxy"
        }

        $env:HTTP_PROXY = $proxy
        $env:HTTPS_PROXY = $proxy
        Write-Host "[proxy] Using Windows system proxy: $proxy"
    } catch {
        Write-Host "[proxy] Failed to read Windows system proxy, continue without proxy: $($_.Exception.Message)" -ForegroundColor Yellow
    }
}

function Ensure-Go {
    $go = Get-Command go -ErrorAction SilentlyContinue
    if (-not $go) {
        throw "Go is not installed or not in PATH. Please install Go 1.25+ first: https://go.dev/dl/"
    }
    $version = (& go version)
    Write-Host "[go] $version"
}

function Open-BrowserWhenReady {
    param([string]$Url)

    Start-Job -ScriptBlock {
        param($TargetUrl)
        for ($i = 0; $i -lt 45; $i++) {
            try {
                $resp = Invoke-WebRequest -Uri $TargetUrl -UseBasicParsing -TimeoutSec 2
                if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 500) {
                    Start-Process $TargetUrl
                    return
                }
            } catch {
                Start-Sleep -Seconds 1
            }
        }
        Start-Process $TargetUrl
    } -ArgumentList $Url | Out-Null
}

Write-Host "GoProxy Windows one-click start" -ForegroundColor Green
Write-Host "Working directory: $PSScriptRoot"

Write-Step "Read Windows system proxy"
Use-SystemProxyIfAvailable

Write-Step "Check Go environment"
Ensure-Go

Write-Step "Download Go dependencies"
& go mod download

Write-Step "Build GoProxy"
$binDir = Join-Path $PSScriptRoot ".bin"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
$exe = Join-Path $binDir "proxygo.exe"
& go build -o $exe .

Write-Step "Start WebUI"
$url = "http://localhost:7778"
Write-Host "WebUI: $url"
Write-Host "Default password: goproxy (override with WEBUI_PASSWORD)"
Write-Host "HTTP proxy: 7777 random / 7776 lowest-latency"
Write-Host "SOCKS5 proxy: 7779 random / 7780 lowest-latency"
Write-Host ""
Write-Host "Service is running in the foreground. Close this window or press Ctrl+C to stop."

Open-BrowserWhenReady -Url $url
& $exe
