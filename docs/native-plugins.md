# 原生插件

`native` 能力允许项目附带 C/C++/Rust 共享库，通过稳定的 C ABI 暴露 `native.<module>.<function>` 给 Lua。构建期校验 ELF（架构、对齐、导出符号、依赖白名单），运行时由 JNI shim `dlopen` 加载；不需要为每个插件编写 Java。

> 插件是**受信任代码**，与壳运行在同一进程，拥有应用的全部权限，没有崩溃隔离。只加载自己构建或审计过的库。

## 快速开始

```bash
# 1. 编译示例插件（需要 Android NDK）
ANDROID_NDK_ROOT=/path/to/ndk examples/native-echo/build.sh

# 2. 重新构建 shell（含三 ABI 的 JNI shim）
ANDROID_NDK_ROOT=/path/to/ndk scripts/build-shell.sh

# 3. 构建 APK
berba build examples/native-echo
```

Windows 上第 2 步可用 `scripts\build-shell.ps1 -Sdk C:\Android\Sdk -Out payload`（同样编译三 ABI shim，产物与 `build-shell.sh` 逐字节一致）；第 1 步的示例脚本需要在 Git Bash 中运行，或参照 `examples/native-echo/build.sh` 里的编译参数直接调用 NDK `clang`。

`app.json`：

```json
{
  "capabilities": ["native"],
  "native": {
    "plugins": [
      { "name": "echo", "dir": ".", "config": { "greeting": "hi" } }
    ]
  }
}
```

`lua/main.lua`：

```lua
-- 同步：返回 JSON 解析后的 Lua 值；失败返回 nil, err
local value, err = native.echo.greet({ name = "berba" })

-- 异步：回调在 Lua 线程执行
native.echo.greet_later({ name = "berba" }, function(result, err)
  if err then
    log("失败: " .. err)
  else
    log("结果: " .. json.encode(result))
  end
end)

-- 已加载插件与函数清单
local plugins = native.list()
```

## 项目结构

```
myapp/
  app.json
  native/echo/
    spec.json
    lib/
      arm64-v8a/libberba_echo.so
      armeabi-v7a/libberba_echo.so
      x86_64/libberba_echo.so
```

- `dir` 默认为 `native/<name>`，必须位于项目目录内；符号链接逃逸会被拒绝。
- 库文件名固定为 `libberba_<name>.so`，放在 `<dir>/lib/<abi>/`。
- 支持 `arm64-v8a`、`armeabi-v7a`、`x86_64`；实际 APK 取所有插件 ABI 的**交集**，交集为空构建失败。
- 缺少 `arm64-v8a` 只告警，但 Play 商店与多数新设备要求 64 位。

## spec.json

```json
{
  "name": "echo",
  "abiVersion": 1,
  "version": "1.0.0",
  "modules": {
    "echo": {
      "functions": {
        "greet": { "async": false },
        "greet_later": { "async": true }
      }
    }
  }
}
```

- `name`：`^[a-z][a-z0-9_]{0,31}$`，不能是 `jni`。
- `abiVersion`：当前固定为 `1`。
- `modules`：模块名不能与内置命名空间（`app`、`web`、`bridge`、`http`、`file`、`store`、`permission`、`ui`、`device`、`json`、`notify`、`task`、`native`、`list`、`jni`）冲突，跨插件也不能重名。
- 模块与函数会合并进 APK 的 `assets/berba_capabilities.json`；`async: true` 的函数在 Lua 侧必须传入回调。

## C ABI v1

公共头文件：[`include/berba_plugin.h`](../include/berba_plugin.h)。

```c
const char *berba_plugin_describe(void);

int32_t berba_plugin_init(const berba_host *host,
                          const uint8_t *config, uint32_t config_length);

int32_t berba_plugin_call(const char *module, const char *function,
                          const uint8_t *args, uint32_t args_length,
                          uint8_t **result, uint32_t *result_length,
                          berba_plugin_complete complete,
                          void *complete_context);

void berba_plugin_free(void *data);
```

- 四个符号都必须导出；编译时使用 `-fvisibility=hidden` 时，头文件中的 `BERBA_PLUGIN_EXPORT` 会为它们设置默认可见性。
- `describe` 返回静态存储、NUL 结尾的 JSON；与构建元数据不一致只记警告，以 `spec.json` 为准。
- `init` 在每个进程内调用一次；`config` 是构建期规范化后的紧凑 JSON，可能为空。
- `call` 的 `args`/`result` 都是 UTF-8 JSON 字节串，不是 NUL 结尾。
- 同步成功：返回 `BERBA_PLUGIN_OK`，`*result` 由插件分配，壳调用 `berba_plugin_free` 释放。
- 异步：返回 `BERBA_PLUGIN_ASYNC`，随后在**任意线程恰好调用一次** `complete`。`complete` 的 `data` 归壳所有，回调返回后由壳释放。
- 同步失败：返回负错误码：

| 常量 | 值 | 含义 |
| --- | --- | --- |
| `BERBA_PLUGIN_ERR` | -1 | 一般错误 |
| `BERBA_PLUGIN_ERR_ARG` | -2 | 参数错误 |
| `BERBA_PLUGIN_ERR_STATE` | -3 | 状态/ABI 不匹配 |
| `BERBA_PLUGIN_ERR_UNSUPPORTED` | -4 | 未知模块或函数 |

- `args` 只在本次调用期间有效；异步函数必须自行复制。
- 同步调用在 **Lua 线程**执行，插件必须快速返回；耗时工作请声明为 `async`。

## 运行时语义

- **同步**：`local value, err = native.m.fn(args)`。结果按 JSON 解析；不是合法 JSON 时按字符串返回；`null` 映射为 `nil`。立即失败返回 `nil, err`。
- **异步**：`native.m.fn(args, callback)`。调用在 IO 线程执行，返回后回调以 `callback(result, err)` 的形式 post 回 Lua 线程。声明为 `async` 的函数若同步返回 `OK`，也会走同一回调。
- **JSON 映射**：Lua table/array 通过 `json.encode` 规则转成 JSON；连续 1..n 键为数组。
- **上限**：参数 1MB、结果 8MB、`config` 64KB、最多 256 个并发异步调用、30 秒超时。超时或迟到/重复的完成会丢弃，超时会以错误回调。
- **失败降级**：某个插件加载、`init` 或 `describe` 失败时，只把该插件的函数替换为抛错桩，其他插件继续可用；`native.list()` 中 `ok=false` 并带 `error`。

`native.list()` 返回数组：

```lua
{
  {
    name = "echo",
    lib = "libberba_echo.so",
    ok = true,
    modules = {
      echo = { functions = { greet = { async = false }, greet_later = { async = true } } }
    }
  }
}
```

## 构建与打包

- 启用 `native.plugins` 时必须在 `capabilities` 中声明 `native`。
- 构建期校验：`ET_DYN`、ELF class/machine 与 ABI 匹配、`PT_LOAD` 对齐（64 位 ≥16KB，32 位 ≥4KB）、四个必需导出符号、`DT_NEEDED` 白名单。
- 依赖白名单：`libc.so`、`libm.so`、`libdl.so`、`liblog.so`。**C++ 插件必须静态链接 STL**（`c++_static` 或 `-static-libstdc++`）；链接 `libc++_shared.so` 会被拒绝。
- 编译参数（`scripts/build-shell.*` 与 `examples/native-echo/build.sh` 使用同一组，自建时请保持一致）：
  `--target=<tri>24 -shared -fPIC -O2 -fvisibility=hidden -I include -Wl,--no-undefined -Wl,-z,max-page-size=16384 -Wl,--build-id=none -ffile-prefix-map=<源码目录>=.`，shim 另加 `-llog -ldl -lm`。缺少 `--build-id=none` 或 `-ffile-prefix-map` 会破坏逐字节可复现构建。
- 有插件时 Manifest 设置 `android:extractNativeLibs="true"`，插件库与 `libberba_jni.so` 一起从 `nativeLibraryDir` 加载。
- APK 中所有 `lib/**/*.so` 不压缩、16KB 对齐，满足 Android 15+ 的 16KB page size 要求。
- `payload/native/<abi>/libberba_jni.so` 由 `scripts/build-shell.*` 在检测到 NDK 时编译；缺少 NDK 时脚本告警并保留已有 shim。`berba build` 在启用原生插件但缺少对应 ABI shim 时直接失败。
- shim 与示例插件用 NDK r26b（`26.1.10909125`，clang 17）验证，CI 固定同一版本；换用其他版本需自行确认 `--build-id=none` 与 16KB 对齐产物一致。
- 正式发行包（`berba release` / `package-dist`）必须包含 shim 与公共头文件；`versions.json` 记录 shim 哈希，`berba doctor` 会校验。

## 示例插件与调试

- [`examples/native-echo`](../examples/native-echo)：同步 `greet` + 跨线程 `greet_later`。
- 插件日志通过 `berba_host.log` 写入 Logcat，tag 为 `berba-plugin`；Lua 侧 `log` 仍为 `berba`。
- 插件内不要缓存宿主的栈内存；异步完成前不要释放 `complete_context` 指向的数据。
- 不要 `dlclose` 或尝试卸载插件；壳故意不卸载已加载的库。

## dev 差异

浏览器 dev 运行时不加载任何 `.so`：它按 `app.json` 的 `capabilities` 与 `native.plugins` 安装 `native` 表——`native.list()` 返回 `app.json` 声明的插件、模块与函数（每项 `ok=false`、`error="原生插件仅在设备上可用"`），`native.<module>.<function>` 一律抛“原生插件仅在设备上可用（native.<module>.<function>）”。请用 `dev/mock.js` 伪造设备结果，或在真机上调试原生代码。未声明 `native` 能力时 `native` 表不存在，与设备一致。

## 安全提示

- 插件属于项目代码，绕过 Lua 沙箱；可以访问任意 Android API/文件/网络。只加载自己构建或审计的库。
- `config` 会写入 APK，任何人都能解包读取；**禁止放密钥**。
- 插件崩溃会带崩整个应用；当前不做进程外隔离。
