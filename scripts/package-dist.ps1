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

Write-Host "==> 写入 versions.json"
$prevEap = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$aapt2Version = (& $Aapt2 version 2>&1 | Out-String).Trim()
$ErrorActionPreference = $prevEap
if (-not $aapt2Version) { Die "无法获取 aapt2 版本" }
$aapt2Version = ($aapt2Version -split "`r?`n")[0].Trim()
$aapt2Version = $aapt2Version -replace '^.* : ', ''
$versions = [ordered]@{
    version    = $Version
    aapt2      = [ordered]@{
        version = $aapt2Version
        sha256  = (Get-FileHash -LiteralPath (Join-Path $Stage "tools\aapt2.exe") -Algorithm SHA256).Hash.ToLowerInvariant()
    }
    androidJar = [ordered]@{
        version = $Platform
        sha256  = (Get-FileHash -LiteralPath (Join-Path $Stage "tools\android.jar") -Algorithm SHA256).Hash.ToLowerInvariant()
    }
}
$json = $versions | ConvertTo-Json -Depth 5
[IO.File]::WriteAllText((Join-Path $Stage "tools\versions.json"), $json + "`n", [Text.UTF8Encoding]::new($false))

Write-Host "==> 复制文档"
foreach ($item in @("NOTICE", "README.md")) {
    $path = Join-Path $Root $item
    if (Test-Path -LiteralPath $path) { Copy-Item -LiteralPath $path -Destination $Stage }
}
$docs = Join-Path $Root "docs"
if (Test-Path -LiteralPath $docs) {
    Copy-Item -LiteralPath $docs -Destination $Stage -Recurse
}

Write-Host "==> 生成 zip"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
if (Test-Path -LiteralPath $Zip) { Remove-Item -LiteralPath $Zip -Force }
# 不用 Compress-Archive：Windows PowerShell 5.1 会写入反斜杠路径，跨平台解压失败。
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zipStream = [IO.File]::Open($Zip, [IO.FileMode]::Create)
try {
    $archive = New-Object IO.Compression.ZipArchive($zipStream, [IO.Compression.ZipArchiveMode]::Create)
    try {
        $prefixLen = $Stage.TrimEnd('\').Length + 1
        Get-ChildItem -LiteralPath $Stage -Recurse -File | Sort-Object FullName | ForEach-Object {
            $rel = $_.FullName.Substring($prefixLen).Replace('\', '/')
            $entry = $archive.CreateEntry($rel, [IO.Compression.CompressionLevel]::Optimal)
            $entry.LastWriteTime = $_.LastWriteTime
            $input = [IO.File]::OpenRead($_.FullName)
            try {
                $output = $entry.Open()
                try { $input.CopyTo($output) } finally { $output.Dispose() }
            } finally { $input.Dispose() }
        }
    } finally { $archive.Dispose() }
} finally { $zipStream.Dispose() }

$zipHash = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant()
$sizeMB = [math]::Round((Get-Item -LiteralPath $Zip).Length / 1MB, 1)
Write-Host ""
Write-Host "完成: $Zip ($sizeMB MB)"
Write-Host "SHA256: $zipHash"
Write-Host "解压后运行: berba.exe doctor"
