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

echo "==> JVM 冒烟测试"
TEST_SRC="$ROOT/shell/app/src/test/java"
if [ -d "$TEST_SRC" ]; then
  rm -rf "$WORK/test-classes"
  mkdir -p "$WORK/test-classes"
  TESTS=()
  while IFS= read -r -d '' f; do
    TESTS+=("$f")
  done < <(find "$TEST_SRC" -name '*.java' -print0 | sort -z)
  if [ ${#TESTS[@]} -gt 0 ]; then
    "$JAVAC" --release 17 -encoding UTF-8 \
      -cp "$WORK_ARG/classes$CPSEP$ANDROID_JAR_ARG$CPSEP$LUAJ_ARG" \
      -d "$WORK/test-classes" "${TESTS[@]}"
    JAVA_CMD="${JAVA_BIN}java"
    command -v "$JAVA_CMD" >/dev/null 2>&1 || die "找不到 java（需要完整 JDK，可设置 JAVA_HOME）"
    TEST_CP="$WORK_ARG/test-classes$CPSEP$WORK_ARG/classes$CPSEP$ANDROID_JAR_ARG$CPSEP$LUAJ_ARG"
    for test in "${TESTS[@]}"; do
      rel="${test#"$TEST_SRC"/}"
      case "$rel" in
        *Test.java) ;;
        *) continue ;;
      esac
      cls="${rel%.java}"
      cls="${cls//\//.}"
      "$JAVA_CMD" -cp "$TEST_CP" "$cls"
    done
  fi
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

[ -f "$ROOT/shell/shell.json" ] || die "缺少 shell/shell.json（运行 go generate ./internal/capability）"
cp "$ROOT/shell/shell.json" "$OUT/shell.json"

# NDK 可选：缺少时跳过 shim，仅影响启用原生插件的构建。
NDK="${ANDROID_NDK_ROOT:-${ANDROID_NDK_HOME:-}}"
if [ -z "$NDK" ] && [ -d "$SDK/ndk" ]; then
  NDK="$SDK/ndk/$(ls -1 "$SDK/ndk" | sort -V | tail -1)"
fi

hostpath() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  else
    printf '%s' "$1"
  fi
}

if [ -z "$NDK" ] || [ ! -d "$NDK" ]; then
  echo "警告: 未找到 Android NDK（设置 ANDROID_NDK_ROOT 或安装到 \$SDK/ndk），跳过 JNI shim" >&2
  echo "      已有 payload/native 保持不变；启用原生插件的构建需要 shim。" >&2
else
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*) NDK_HOST="windows-x86_64" ;;
    Darwin*) NDK_HOST="darwin-x86_64" ;;
    *) NDK_HOST="linux-x86_64" ;;
  esac
  CLANG="$NDK/toolchains/llvm/prebuilt/$NDK_HOST/bin/clang"
  [ -f "$CLANG" ] || CLANG="$CLANG.exe"
  if [ ! -f "$CLANG" ]; then
    echo "警告: 找不到 NDK clang（$NDK/toolchains/llvm/prebuilt/$NDK_HOST），跳过 JNI shim" >&2
  else
    SHIM_SRC="$ROOT/shell/jni/berba_jni.c"
    [ -f "$SHIM_SRC" ] || die "找不到 JNI shim 源码: $SHIM_SRC"
    echo "==> 编译 JNI shim（arm64-v8a / armeabi-v7a / x86_64）"
    rm -rf "$OUT/native"
    for pair in \
      "arm64-v8a:aarch64-linux-android" \
      "armeabi-v7a:armv7a-linux-androideabi" \
      "x86_64:x86_64-linux-android"; do
      abi="${pair%%:*}"
      triple="${pair##*:}"
      mkdir -p "$OUT/native/$abi"
      "$CLANG" "--target=${triple}24" -shared -fPIC -O2 -fvisibility=hidden \
        -I "$(hostpath "$ROOT/shell/jni")" -I "$(hostpath "$ROOT/include")" \
        -Wl,--no-undefined -Wl,-z,max-page-size=16384 -Wl,--build-id=none \
        "-ffile-prefix-map=$(hostpath "$ROOT")=." \
        -o "$(hostpath "$OUT/native/$abi/libberba_jni.so")" \
        "$(hostpath "$SHIM_SRC")" -llog -ldl -lm
    done
    ls -l "$OUT"/native/*/libberba_jni.so
  fi
fi

echo "==> 完成"
ls -l "$OUT"/classes*.dex "$OUT/shell.json"
