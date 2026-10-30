#!/usr/bin/env bash
# 编译 native-echo 示例插件到 lib/<abi>/libberba_echo.so。
# 用法: ANDROID_NDK_ROOT=/path/to/ndk examples/native-echo/build.sh [输出目录]
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="${1:-$DIR/lib}"
SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"

NDK="${ANDROID_NDK_ROOT:-${ANDROID_NDK_HOME:-}}"
if [ -z "$NDK" ] && [ -n "$SDK" ] && [ -d "$SDK/ndk" ]; then
  NDK="$SDK/ndk/$(ls -1 "$SDK/ndk" | sort -V | tail -1)"
fi
[ -n "$NDK" ] && [ -d "$NDK" ] || {
  echo "错误: 未找到 Android NDK（设置 ANDROID_NDK_ROOT）" >&2
  exit 1
}

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*) NDK_HOST="windows-x86_64" ;;
  Darwin*) NDK_HOST="darwin-x86_64" ;;
  *) NDK_HOST="linux-x86_64" ;;
esac
CLANG="$NDK/toolchains/llvm/prebuilt/$NDK_HOST/bin/clang"
[ -f "$CLANG" ] || CLANG="$CLANG.exe"
[ -f "$CLANG" ] || {
  echo "错误: 找不到 NDK clang: $NDK/toolchains/llvm/prebuilt/$NDK_HOST" >&2
  exit 1
}

hostpath() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  else
    printf '%s' "$1"
  fi
}

rm -rf "$OUT"
for pair in \
  "arm64-v8a:aarch64-linux-android" \
  "armeabi-v7a:armv7a-linux-androideabi" \
  "x86_64:x86_64-linux-android"; do
  abi="${pair%%:*}"
  triple="${pair##*:}"
  mkdir -p "$OUT/$abi"
  echo "==> 编译 libberba_echo.so ($abi)"
  "$CLANG" "--target=${triple}24" -shared -fPIC -O2 -fvisibility=hidden -Wall -Wextra \
    -I "$(hostpath "$DIR/../../include")" \
    -Wl,--no-undefined -Wl,-z,max-page-size=16384 -Wl,--build-id=none \
    "-ffile-prefix-map=$(hostpath "$DIR")=." \
    -o "$(hostpath "$OUT/$abi/libberba_echo.so")" \
    "$(hostpath "$DIR/plugin.c")"
done
ls -l "$OUT"/*/libberba_echo.so
