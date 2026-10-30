#ifndef BERBA_PLUGIN_H
#define BERBA_PLUGIN_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* 插件在 -fvisibility=hidden 下仍需导出的符号。 */
#if defined(__GNUC__) && __GNUC__ >= 4
#define BERBA_PLUGIN_EXPORT __attribute__((visibility("default")))
#else
#define BERBA_PLUGIN_EXPORT
#endif

/* 与壳协商的 ABI 版本；不匹配的插件会被拒绝加载。 */
#define BERBA_PLUGIN_ABI_VERSION 1

/* berba_plugin_call 的返回值。负数为同步错误；BERBA_PLUGIN_ASYNC 表示稍后回调。 */
#define BERBA_PLUGIN_OK 0
#define BERBA_PLUGIN_ASYNC 1
#define BERBA_PLUGIN_ERR (-1)
#define BERBA_PLUGIN_ERR_ARG (-2)
#define BERBA_PLUGIN_ERR_STATE (-3)
#define BERBA_PLUGIN_ERR_UNSUPPORTED (-4)

typedef struct berba_host {
    uint32_t abi_version;
    void *context;
    /* message 不是 NUL 结尾，长度由 length 给出。level: 0=debug, 1=info, 2=warn, 3=error。 */
    void (*log)(void *context, int32_t level, const char *message, uint32_t length);
} berba_host;

/* 完成异步调用；data 归壳所有，回调返回后由壳调用 berba_plugin_free。 */
typedef void (*berba_plugin_complete)(void *context, int32_t status,
                                      uint8_t *data, uint32_t length);

/*
 * 返回静态存储的 NUL 结尾 JSON，例如：
 * {"name":"echo","version":"1.0.0","modules":{"echo":["call"]}}
 */
typedef const char *(*berba_plugin_describe_fn)(void);

/* 进程内调用一次；config 为 UTF-8 JSON（可能为空），返回 0 表示成功。 */
typedef int32_t (*berba_plugin_init_fn)(const berba_host *host,
                                        const uint8_t *config,
                                        uint32_t config_length);

/*
 * args 只在本次调用期间有效，异步函数必须自行复制。
 * 同步成功：返回 BERBA_PLUGIN_OK，*result 由壳用 berba_plugin_free 释放。
 * 异步：返回 BERBA_PLUGIN_ASYNC，complete 必须在任意线程恰好调用一次；
 *       complete 的 data 由壳在回调返回后释放。
 * 失败：返回负错误码（异步可在 complete 的 status 中返回负错误码）。
 */
typedef int32_t (*berba_plugin_call_fn)(const char *module,
                                        const char *function,
                                        const uint8_t *args,
                                        uint32_t args_length,
                                        uint8_t **result,
                                        uint32_t *result_length,
                                        berba_plugin_complete complete,
                                        void *complete_context);

typedef void (*berba_plugin_free_fn)(void *data);

BERBA_PLUGIN_EXPORT const char *berba_plugin_describe(void);
BERBA_PLUGIN_EXPORT int32_t berba_plugin_init(const berba_host *host,
                                              const uint8_t *config,
                                              uint32_t config_length);
BERBA_PLUGIN_EXPORT int32_t berba_plugin_call(const char *module,
                                              const char *function,
                                              const uint8_t *args,
                                              uint32_t args_length,
                                              uint8_t **result,
                                              uint32_t *result_length,
                                              berba_plugin_complete complete,
                                              void *complete_context);
BERBA_PLUGIN_EXPORT void berba_plugin_free(void *data);

#ifdef __cplusplus
}
#endif

#endif
