# 组装 Windows x64 发行包（CLI + aapt2 + android.jar + shell payload）。
# 需在本机安装 Android SDK（platform 35 + build-tools）与 JDK 17+。
#
# 用法: scripts\package-dist.ps1 [-Version 0.1.0] [-Sdk C:\Android\Sdk]
# 产物: dist\windows-amd64\ 与 dist\berba-<版本>-windows-amd64.zip
param(
    [string]$Version = "0.1.0",
    [string]$Sdk = "",
    [string]$Platform = "android-35",
    [string]$BuildTools = "",
    [string]$OutDir = "",
    [string]$Luaj = ""
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
if (-not $Sdk) { $Sdk = $env:ANDROID_SDK_ROOT; if (-not $Sdk) { $Sdk = $env:ANDROID_HOME } }
if (-not $OutDir) { $OutDir = Join-Path $Root "dist" }
if (-not $Luaj) { $Luaj = Join-Path $Root ".tools\luaj\luaj-jse-3.0.1.jar" }

function Die([string]$Message) {
    Write-Host "错误: $Message" -ForegroundColor Red
    exit 1
}

if (-not $Sdk) { Die "请设置 ANDROID_SDK_ROOT/ANDROID_HOME 或用 -Sdk 指定" }
if (-not (Test-Path -LiteralPath $Sdk)) { Die "SDK 目录不存在: $Sdk" }

$AndroidJar = Join-Path $Sdk "platforms\$Platform\android.jar"
if (-not (Test-Path -LiteralPath $AndroidJar)) { Die "找不到 $AndroidJar" }

if (-not $BuildTools) {
    $btRoot = Join-Path $Sdk "build-tools"
    $BuildTools = (Get-ChildItem -LiteralPath $btRoot -Directory |
        Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName "aapt2.exe") } |
        Sort-Object {
            $n = @(($_.Name -replace '[^0-9.]', '').Split('.') | Where-Object { $_ -ne '' })
            while ($n.Count -lt 3) { $n += '0' }
            [version]("$($n[0]).$($n[1]).$($n[2])")
        } |
        Select-Object -Last 1).Name
if (-not $BuildTools) { Die "在 $btRoot 中找不到包含 aapt2.exe 的 build-tools" }
}
$Aapt2 = Join-Path $Sdk "build-tools\$BuildTools\aapt2.exe"
if (-not (Test-Path -LiteralPath $Aapt2)) { Die "找不到 $Aapt2" }

$Stage = Join-Path $OutDir "windows-amd64"
$Zip = Join-Path $OutDir "berba-$Version-windows-amd64.zip"

Write-Host "==> 清理 $Stage"
if (Test-Path -LiteralPath $Stage) { Remove-Item -LiteralPath $Stage -Recurse -Force }
New-Item -ItemType Directory -Force -Path (Join-Path $Stage "tools"), (Join-Path $Stage "payload") | Out-Null

Write-Host "==> 构建 berba.exe ($Version)"
Push-Location $Root
try {
    & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o (Join-Path $Stage "berba.exe") ./cmd/berba
    if ($LASTEXITCODE -ne 0) { Die "go build 失败 (exit $LASTEXITCODE)" }
} finally {
    Pop-Location
}

Write-Host "==> 复制工具链"
Copy-Item -LiteralPath $Aapt2 -Destination (Join-Path $Stage "tools\aapt2.exe")
Copy-Item -LiteralPath $AndroidJar -Destination (Join-Path $Stage "tools\android.jar")

Write-Host "==> 编译 shell payload"
& (Join-Path $PSScriptRoot "build-shell.ps1") -Out (Join-Path $Stage "payload") -Sdk $Sdk -Platform $Platform -BuildTools $BuildTools -Luaj $Luaj
if ($LASTEXITCODE -ne 0) { Die "build-shell 失败" }

Write-Host "==> 生成发行包（versions.json + 文档 + zip）"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
& (Join-Path $Stage "berba.exe") release --home $Stage --out $OutDir --version $Version --platform $Platform --docs $Root
if ($LASTEXITCODE -ne 0) { Die "berba release 失败 (exit $LASTEXITCODE)" }

$zipHash = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
$sizeMB = [math]::Round((Get-Item -LiteralPath $Zip).Length / 1MB, 1)
Write-Host ""
Write-Host "完成: $Zip ($sizeMB MB)"
Write-Host "SHA256: $zipHash"
Write-Host "解压后运行: berba.exe doctor"
