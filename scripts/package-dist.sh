#!/usr/bin/env bash
# 组装当前平台（Linux/macOS）的发行包。
# 依赖: Go 1.24+、JDK 17+、Android SDK、LuaJ jar。
#
# 用法: VERSION=0.1.0 scripts/package-dist.sh
# 环境变量:
#   VERSION        版本号（默认 0.1.0）
#   ANDROID_SDK_ROOT / ANDROID_HOME
#   PLATFORM       platform 目录名（默认 android-35）
#   BUILD_TOOLS    build-tools 版本（默认最新）
#   LUAJ_JAR       LuaJ jar
#   OUTDIR         输出目录（默认 <repo>/dist）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-0.1.0}"
SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
PLATFORM="${PLATFORM:-android-35}"
BUILD_TOOLS="${BUILD_TOOLS:-}"
LUAJ="${LUAJ_JAR:-$ROOT/.tools/luaj/luaj-jse-3.0.1.jar}"
OUTDIR="${OUTDIR:-$ROOT/dist}"

die() { echo "错误: $*" >&2; exit 1; }

[ -n "$SDK" ] || die "请设置 ANDROID_SDK_ROOT 或 ANDROID_HOME"
[ -d "$SDK" ] || die "SDK 目录不存在: $SDK"
[ -f "$LUAJ" ] || die "找不到 LuaJ jar: $LUAJ"

HOST_OS="$(go env GOHOSTOS)"
HOST_ARCH="$(go env GOHOSTARCH)"
TARGET_OS="${GOOS_TARGET:-$HOST_OS}"
TARGET_ARCH="${GOARCH_TARGET:-$HOST_ARCH}"
[ "$TARGET_OS" = "$HOST_OS" ] && [ "$TARGET_ARCH" = "$HOST_ARCH" ] ||
  die "不支持交叉打包：aapt2 必须与目标平台一致，请在对应平台运行"

ANDROID_JAR="$SDK/platforms/$PLATFORM/android.jar"
[ -f "$ANDROID_JAR" ] || die "找不到 $ANDROID_JAR"

if [ -z "$BUILD_TOOLS" ]; then
  BUILD_TOOLS="$(ls -1 "$SDK/build-tools" | sort -V | tail -1)"
fi
AAPT2="$SDK/build-tools/$BUILD_TOOLS/aapt2"
[ -f "$AAPT2" ] || AAPT2="$AAPT2.exe"
[ -f "$AAPT2" ] || die "找不到 aapt2: $SDK/build-tools/$BUILD_TOOLS/"

EXE=""
[ "$TARGET_OS" = "windows" ] && EXE=".exe"
NAME="berba-$VERSION-$TARGET_OS-$TARGET_ARCH"
STAGE="$OUTDIR/$TARGET_OS-$TARGET_ARCH"
ZIP="$OUTDIR/$NAME.zip"

hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "找不到 sha256sum/shasum"
  fi
}

echo "==> 清理 $STAGE"
rm -rf "$STAGE"
mkdir -p "$STAGE/tools" "$STAGE/payload"

echo "==> 构建 berba$EXE ($VERSION)"
(cd "$ROOT" && CGO_ENABLED=0 GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" \
  go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
  -o "$STAGE/berba$EXE" ./cmd/berba)

echo "==> 复制工具链"
cp "$AAPT2" "$STAGE/tools/$(basename "$AAPT2")"
cp "$ANDROID_JAR" "$STAGE/tools/android.jar"

echo "==> 编译 shell payload"
ANDROID_SDK_ROOT="$SDK" PLATFORM="$PLATFORM" BUILD_TOOLS="$BUILD_TOOLS" \
  OUT="$STAGE/payload" LUAJ_JAR="$LUAJ" bash "$ROOT/scripts/build-shell.sh"

echo "==> 生成发行包（versions.json + 文档 + zip）"
mkdir -p "$OUTDIR"
"$STAGE/berba$EXE" release --home "$STAGE" --out "$OUTDIR" --version "$VERSION" \
  --platform "$PLATFORM" --docs "$ROOT"

echo ""
echo "完成: $ZIP ($(du -h "$ZIP" | awk '{print $1}'))"
echo "SHA256: $(hash_file "$ZIP")"
echo "解压后运行: berba$EXE doctor"
