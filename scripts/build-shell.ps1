# 编译 Android shell 并生成 payload/classes.dex。
# 依赖: JDK 17+、Android SDK（platform + build-tools）、LuaJ jar。
#
# 用法: scripts\build-shell.ps1 [-Out <目录>] [-Sdk <SDK>] [-Platform android-35] [-BuildTools <版本>] [-Luaj <jar>]
param(
    [string]$Out = "",
    [string]$Sdk = "",
    [string]$Platform = "android-35",
    [string]$BuildTools = "",
    [string]$Luaj = ""
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
if (-not $Out) { $Out = Join-Path $Root "payload" }
if (-not $Sdk) { $Sdk = $env:ANDROID_SDK_ROOT; if (-not $Sdk) { $Sdk = $env:ANDROID_HOME } }
if (-not $Luaj) { $Luaj = Join-Path $Root ".tools\luaj\luaj-jse-3.0.1.jar" }
$Work = Join-Path $Root ".tools\shellbuild"
$Src = Join-Path $Root "shell\app\src\main\java"

function Die([string]$Message) {
    Write-Host "错误: $Message" -ForegroundColor Red
    exit 1
}

if (-not $Sdk) { Die "请设置 ANDROID_SDK_ROOT/ANDROID_HOME 或用 -Sdk 指定" }
if (-not (Test-Path -LiteralPath $Sdk)) { Die "SDK 目录不存在: $Sdk" }
if (-not (Test-Path -LiteralPath $Src)) { Die "找不到 shell 源码: $Src" }
if (-not (Test-Path -LiteralPath $Luaj)) { Die "找不到 LuaJ jar: $Luaj" }

$AndroidJar = Join-Path $Sdk "platforms\$Platform\android.jar"
if (-not (Test-Path -LiteralPath $AndroidJar)) { Die "找不到 $AndroidJar（请安装 platform $Platform）" }

if (-not $BuildTools) {
    $btRoot = Join-Path $Sdk "build-tools"
    if (-not (Test-Path -LiteralPath $btRoot)) { Die "找不到 $btRoot" }
    $BuildTools = (Get-ChildItem -LiteralPath $btRoot -Directory |
        Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName "d8.bat") } |
        Sort-Object {
            $n = @(($_.Name -replace '[^0-9.]', '').Split('.') | Where-Object { $_ -ne '' })
            while ($n.Count -lt 3) { $n += '0' }
            [version]("$($n[0]).$($n[1]).$($n[2])")
        } |
        Select-Object -Last 1).Name
}
$D8 = Join-Path $Sdk "build-tools\$BuildTools\d8.bat"
if (-not (Test-Path -LiteralPath $D8)) {
    $D8 = Join-Path $Sdk "build-tools\$BuildTools\d8"
}
if (-not (Test-Path -LiteralPath $D8)) { Die "找不到 d8: $D8" }

# 某些 Windows 环境只有 javapath 垫片（无 jar），用 java.home 找完整 JDK。
$JavaHomeReal = $env:JAVA_HOME
if (-not $JavaHomeReal -or -not (Test-Path -LiteralPath (Join-Path $JavaHomeReal "bin\javac.exe"))) {
    $prevEap = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $props = & java -XshowSettings:properties -version 2>&1 | Out-String
    $ErrorActionPreference = $prevEap
    if ($props -match 'java\.home = (.+)') {
        $JavaHomeReal = $Matches[1].Trim()
    }
}
if ($JavaHomeReal -and (Test-Path -LiteralPath (Join-Path $JavaHomeReal "bin\javac.exe"))) {
    $Javac = Join-Path $JavaHomeReal "bin\javac.exe"
    $Jar = Join-Path $JavaHomeReal "bin\jar.exe"
    $Java = Join-Path $JavaHomeReal "bin\java.exe"
} else {
    $Javac = "javac"
    $Jar = "jar"
    $Java = "java"
}

Write-Host "==> 清理 $Work"
$Classes = Join-Path $Work "classes"
$Dex = Join-Path $Work "dex"
foreach ($p in @($Classes, $Dex)) {
    if (Test-Path -LiteralPath $p) { Remove-Item -LiteralPath $p -Recurse -Force }
}
New-Item -ItemType Directory -Force -Path $Classes, $Dex | Out-Null

Write-Host "==> 编译 Java（--release 17）"
$sources = Get-ChildItem -LiteralPath $Src -Recurse -Filter *.java | ForEach-Object { $_.FullName }
& $Javac --release 17 -encoding UTF-8 -cp "$AndroidJar;$Luaj" -d $Classes @sources
if ($LASTEXITCODE -ne 0) { Die "javac 失败 (exit $LASTEXITCODE)" }

Write-Host "==> Lua 沙箱冒烟测试"
$TestSrc = Join-Path $Root "shell\app\src\test\java\com\berba\shell\LuaSandboxTest.java"
if (Test-Path -LiteralPath $TestSrc) {
    $TestClasses = Join-Path $Work "test-classes"
    if (Test-Path -LiteralPath $TestClasses) { Remove-Item -LiteralPath $TestClasses -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $TestClasses | Out-Null
    & $Javac --release 17 -encoding UTF-8 -cp $Luaj -d $TestClasses `
        (Join-Path $Src "com\berba\shell\LuaSandbox.java") $TestSrc
    if ($LASTEXITCODE -ne 0) { Die "沙箱测试编译失败 (exit $LASTEXITCODE)" }
    & $Java -cp "$TestClasses;$Luaj" com.berba.shell.LuaSandboxTest
    if ($LASTEXITCODE -ne 0) { Die "Lua 沙箱冒烟测试失败 (exit $LASTEXITCODE)" }
}

Write-Host "==> 打包 shell.jar"
$ShellJar = Join-Path $Work "shell.jar"
if (Test-Path -LiteralPath $ShellJar) { Remove-Item -LiteralPath $ShellJar -Force }
& $Jar cf $ShellJar -C $Classes .
if ($LASTEXITCODE -ne 0) { Die "jar 失败 (exit $LASTEXITCODE)" }

Write-Host "==> d8 生成 dex（min-api 24，含 LuaJ）"
& $D8 --release --min-api 24 --lib $AndroidJar --output $Dex $ShellJar $Luaj
if ($LASTEXITCODE -ne 0) { Die "d8 失败 (exit $LASTEXITCODE)" }

New-Item -ItemType Directory -Force -Path $Out | Out-Null
Get-ChildItem -LiteralPath $Out -Filter "classes*.dex" | Remove-Item -Force
$dexes = Get-ChildItem -LiteralPath $Dex -Filter "classes*.dex"
if (-not $dexes) { Die "d8 未生成 classes.dex" }
$dexes | Copy-Item -Destination $Out -Force

Write-Host "==> 完成"
Get-ChildItem -LiteralPath $Out -Filter "classes*.dex" | Format-Table Name, Length
