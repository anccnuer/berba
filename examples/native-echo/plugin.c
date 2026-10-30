/* native-echo 示例插件：同步 greet 与跨线程 greet_later。 */
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "berba_plugin.h"

static const char *DESCRIBE =
        "{\"name\":\"echo\",\"version\":\"1.0.0\","
        "\"modules\":{\"echo\":[\"greet\",\"greet_later\"]}}";

static uint32_t g_config_length;

/* 生成 {"<prefix>":<args>}；args 必须是合法 JSON。 */
static char *wrap(const char *prefix, const uint8_t *args, uint32_t length) {
    const char *payload = args != NULL && length > 0 ? (const char *)args : "null";
    size_t payload_length = args != NULL && length > 0 ? length : 4;
    size_t prefix_length = strlen(prefix);
    char *out = (char *)malloc(prefix_length + payload_length + 2);
    if (out == NULL) {
        return NULL;
    }
    memcpy(out, prefix, prefix_length);
    memcpy(out + prefix_length, payload, payload_length);
    out[prefix_length + payload_length] = '}';
    out[prefix_length + payload_length + 1] = '\0';
    return out;
}

typedef struct {
    uint8_t *args;
    uint32_t length;
    berba_plugin_complete complete;
    void *complete_context;
} echo_job;

static void *echo_worker(void *raw) {
    echo_job *job = (echo_job *)raw;
    struct timespec delay = {0, 150 * 1000 * 1000};
    nanosleep(&delay, NULL);
    char *out = wrap("{\"async\":true,\"args\":", job->args, job->length);
    job->complete(job->complete_context, out == NULL ? BERBA_PLUGIN_ERR : BERBA_PLUGIN_OK,
                  (uint8_t *)out, out == NULL ? 0 : (uint32_t)strlen(out));
    free(job->args);
    free(job);
    return NULL;
}

BERBA_PLUGIN_EXPORT const char *berba_plugin_describe(void) {
    return DESCRIBE;
}

BERBA_PLUGIN_EXPORT int32_t berba_plugin_init(const berba_host *host,
                                              const uint8_t *config,
                                              uint32_t config_length) {
    if (host == NULL || host->abi_version != BERBA_PLUGIN_ABI_VERSION) {
        return BERBA_PLUGIN_ERR_STATE;
    }
    if (config_length > 64u * 1024u) {
        return BERBA_PLUGIN_ERR_ARG;
    }
    g_config_length = config_length;
    if (host->log != NULL) {
        char message[64];
        int n = snprintf(message, sizeof(message), "echo init: config %u bytes",
                         (unsigned)config_length);
        host->log(host->context, 1, message, (uint32_t)n);
    }
    (void)config;
    return BERBA_PLUGIN_OK;
}

BERBA_PLUGIN_EXPORT int32_t berba_plugin_call(const char *module,
                                              const char *function,
                                              const uint8_t *args,
                                              uint32_t args_length,
                                              uint8_t **result,
                                              uint32_t *result_length,
                                              berba_plugin_complete complete,
                                              void *complete_context) {
    if (module == NULL || function == NULL) {
        return BERBA_PLUGIN_ERR_ARG;
    }
    if (strcmp(module, "echo") != 0) {
        return BERBA_PLUGIN_ERR_UNSUPPORTED;
    }

    if (strcmp(function, "greet") == 0) {
        if (complete != NULL || result == NULL || result_length == NULL) {
            return BERBA_PLUGIN_ERR_ARG;
        }
        char prefix[64];
        int n = snprintf(prefix, sizeof(prefix),
                         "{\"sync\":true,\"configLength\":%u,\"args\":",
                         (unsigned)g_config_length);
        if (n <= 0 || (size_t)n >= sizeof(prefix)) {
            return BERBA_PLUGIN_ERR;
        }
        char *out = wrap(prefix, args, args_length);
        if (out == NULL) {
            return BERBA_PLUGIN_ERR;
        }
        *result = (uint8_t *)out;
        *result_length = (uint32_t)strlen(out);
        return BERBA_PLUGIN_OK;
    }

    if (strcmp(function, "greet_later") == 0) {
        if (complete == NULL) {
            return BERBA_PLUGIN_ERR_ARG;
        }
        echo_job *job = (echo_job *)calloc(1, sizeof(echo_job));
        if (job == NULL) {
            return BERBA_PLUGIN_ERR;
        }
        if (args_length > 0) {
            job->args = (uint8_t *)malloc(args_length);
            if (job->args == NULL) {
                free(job);
                return BERBA_PLUGIN_ERR;
            }
            memcpy(job->args, args, args_length);
        }
        job->length = args_length;
        job->complete = complete;
        job->complete_context = complete_context;
        pthread_t thread;
        if (pthread_create(&thread, NULL, echo_worker, job) != 0) {
            free(job->args);
            free(job);
            return BERBA_PLUGIN_ERR;
        }
        pthread_detach(thread);
        return BERBA_PLUGIN_ASYNC;
    }

    return BERBA_PLUGIN_ERR_UNSUPPORTED;
}

BERBA_PLUGIN_EXPORT void berba_plugin_free(void *data) {
    free(data);
}
