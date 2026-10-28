#!/usr/bin/env bash
# 编译 Android shell 并生成 payload/classes.dex。
# 依赖: JDK 17+、Android SDK（platform + build-tools）、LuaJ jar。
#
# 用法: scripts/build-shell.sh [输出目录]
# 环境变量:
#   ANDROID_SDK_ROOT / ANDROID_HOME  SDK 根目录
#   PLATFORM                         platform 目录名（默认 android-35）
#   BUILD_TOOLS                      build-tools 版本（默认取最新）
#   LUAJ_JAR                         LuaJ jar 路径
#   OUT                              输出目录（默认 <repo>/payload）
#   WORK                             中间目录（默认 <repo>/.tools/shellbuild）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-${OUT:-$ROOT/payload}}"
SDK="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"
PLATFORM="${PLATFORM:-android-35}"
BUILD_TOOLS="${BUILD_TOOLS:-}"
LUAJ="${LUAJ_JAR:-$ROOT/.tools/luaj/luaj-jse-3.0.1.jar}"
WORK="${WORK:-$ROOT/.tools/shellbuild}"
SRC="$ROOT/shell/app/src/main/java"

die() { echo "错误: $*" >&2; exit 1; }

[ -n "$SDK" ] || die "请设置 ANDROID_SDK_ROOT 或 ANDROID_HOME"
[ -d "$SDK" ] || die "SDK 目录不存在: $SDK"
[ -d "$SRC" ] || die "找不到 shell 源码: $SRC"
[ -f "$LUAJ" ] || die "找不到 LuaJ jar: $LUAJ（设置 LUAJ_JAR 指定）"

ANDROID_JAR="$SDK/platforms/$PLATFORM/android.jar"
[ -f "$ANDROID_JAR" ] || die "找不到 $ANDROID_JAR（请安装 platform $PLATFORM）"

if [ -z "$BUILD_TOOLS" ]; then
  [ -d "$SDK/build-tools" ] || die "找不到 $SDK/build-tools"
  BUILD_TOOLS="$(ls -1 "$SDK/build-tools" | sort -V | tail -1)"
fi
D8="$SDK/build-tools/$BUILD_TOOLS/d8"
if [ ! -f "$D8" ]; then
  D8="$D8.bat"
fi
[ -f "$D8" ] || die "找不到 d8: $SDK/build-tools/$BUILD_TOOLS/d8"

# 某些 Windows 环境只有 javapath 垫片（无 jar），用 java.home 找完整 JDK。
JAVA_BIN=""
if [ -n "${JAVA_HOME:-}" ] && [ -x "$JAVA_HOME/bin/javac" ]; then
  JAVA_BIN="$JAVA_HOME/bin/"
else
  if command -v javac >/dev/null 2>&1 && command -v jar >/dev/null 2>&1; then
    JAVAC="javac"
    JAR="jar"
  else
    JAVA_HOME_REAL="$(java -XshowSettings:properties -version 2>&1 |
      sed -n 's/^[[:space:]]*java\.home = //p' | head -1)"
    if [ -n "$JAVA_HOME_REAL" ] && command -v cygpath >/dev/null 2>&1; then
      JAVA_HOME_REAL="$(cygpath -u "$JAVA_HOME_REAL")"
    fi
    if [ -n "$JAVA_HOME_REAL" ] && [ -x "$JAVA_HOME_REAL/bin/javac" ]; then
      JAVA_BIN="$JAVA_HOME_REAL/bin/"
    fi
  fi
fi
JAVAC="${JAVAC:-${JAVA_BIN}javac}"
JAR="${JAR:-${JAVA_BIN}jar}"
command -v "$JAVAC" >/dev/null 2>&1 || die "找不到 javac（需要 JDK 17+，可设置 JAVA_HOME）"
command -v "$JAR" >/dev/null 2>&1 || die "找不到 jar（需要完整 JDK，可设置 JAVA_HOME）"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    CPSEP=';'
    if command -v cygpath >/dev/null 2>&1; then
      ANDROID_JAR_ARG="$(cygpath -m "$ANDROID_JAR")"
      LUAJ_ARG="$(cygpath -m "$LUAJ")"
      WORK_ARG="$(cygpath -m "$WORK")"
    else
      ANDROID_JAR_ARG="$ANDROID_JAR"
      LUAJ_ARG="$LUAJ"
      WORK_ARG="$WORK"
    fi
    ;;
  *)
    CPSEP=':'
    ANDROID_JAR_ARG="$ANDROID_JAR"
    LUAJ_ARG="$LUAJ"
    WORK_ARG="$WORK"
    ;;
esac

echo "==> 清理 $WORK"
rm -rf "$WORK/classes" "$WORK/dex" "$WORK/shell.jar"
mkdir -p "$WORK/classes" "$WORK/dex"

echo "==> 编译 Java（--release 17）"
SOURCES=()
while IFS= read -r -d '' f; do
  SOURCES+=("$f")
done < <(find "$SRC" -name '*.java' -print0)

"$JAVAC" --release 17 -encoding UTF-8 \
  -cp "$ANDROID_JAR_ARG$CPSEP$LUAJ_ARG" \
  -d "$WORK/classes" "${SOURCES[@]}"

echo "==> Lua 沙箱冒烟测试"
TEST_SRC="$ROOT/shell/app/src/test/java/com/berba/shell/LuaSandboxTest.java"
if [ -f "$TEST_SRC" ]; then
  rm -rf "$WORK/test-classes"
  mkdir -p "$WORK/test-classes"
  "$JAVAC" --release 17 -encoding UTF-8 -cp "$LUAJ_ARG" -d "$WORK/test-classes" \
    "$SRC/com/berba/shell/LuaSandbox.java" "$TEST_SRC"
  JAVA_CMD="${JAVA_BIN}java"
  command -v "$JAVA_CMD" >/dev/null 2>&1 || die "找不到 java（需要完整 JDK，可设置 JAVA_HOME）"
  "$JAVA_CMD" -cp "$WORK_ARG/test-classes$CPSEP$LUAJ_ARG" com.berba.shell.LuaSandboxTest
fi

echo "==> 打包 shell.jar"
"$JAR" cf "$WORK/shell.jar" -C "$WORK/classes" .

echo "==> d8 生成 dex（min-api 24，含 LuaJ）"
"$D8" --release --min-api 24 \
  --lib "$ANDROID_JAR" \
  --output "$WORK/dex" \
  "$WORK/shell.jar" "$LUAJ"

mkdir -p "$OUT"
rm -f "$OUT"/classes*.dex
for dex in "$WORK"/dex/classes*.dex; do
  [ -f "$dex" ] || die "d8 未生成 classes.dex"
  cp "$dex" "$OUT/"
done

echo "==> 完成"
ls -l "$OUT"/classes*.dex
