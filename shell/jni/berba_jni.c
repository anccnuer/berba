#include <jni.h>
#include <android/log.h>
#include <dlfcn.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "berba_plugin.h"

#define TAG "berba-plugin"
#define MAX_CONFIG (64u * 1024u)
#define MAX_ARGS (1u << 20)
#define MAX_RESULT (8u << 20)
#define MAX_NAME 128u

typedef struct {
    void *lib;
    berba_plugin_describe_fn describe;
    berba_plugin_init_fn init;
    berba_plugin_call_fn call;
    berba_plugin_free_fn release;
    pthread_mutex_t lock;
} plugin_handle;

typedef struct {
    JavaVM *vm;
    jlong task_id;
    berba_plugin_free_fn release;
} completion_ctx;

static JavaVM *g_vm;
static jclass g_bridge;
static jmethodID g_dispatch;

static void throw_state(JNIEnv *env, const char *message) {
    jclass cls = (*env)->FindClass(env, "java/lang/IllegalStateException");
    if (cls != NULL) {
        (*env)->ThrowNew(env, cls, message);
        (*env)->DeleteLocalRef(env, cls);
    }
}

static void throw_code(JNIEnv *env, const char *what, int32_t code) {
    char message[160];
    snprintf(message, sizeof(message), "%s（错误码 %d）", what, (int)code);
    throw_state(env, message);
}

static void host_log(void *context, int32_t level, const char *message, uint32_t length) {
    (void)context;
    char buf[1024];
    uint32_t n = length;
    if (n > sizeof(buf) - 1) {
        n = sizeof(buf) - 1;
    }
    if (message != NULL && n > 0) {
        memcpy(buf, message, n);
    }
    buf[n] = '\0';
    int priority = ANDROID_LOG_INFO;
    switch (level) {
        case 0: priority = ANDROID_LOG_DEBUG; break;
        case 1: priority = ANDROID_LOG_INFO; break;
        case 2: priority = ANDROID_LOG_WARN; break;
        default: priority = ANDROID_LOG_ERROR; break;
    }
    __android_log_write(priority, TAG, buf);
}

static plugin_handle *handle_of(jlong raw) {
    return (plugin_handle *)(intptr_t)raw;
}

/* 在任意线程把结果投递回 Java；同时负责释放插件内存。 */
static void complete_dispatch(void *raw, int32_t status, uint8_t *data, uint32_t length) {
    completion_ctx *ctx = (completion_ctx *)raw;
    if (ctx == NULL) {
        return;
    }
    if (length > MAX_RESULT) {
        status = -5;
    }
    JNIEnv *env = NULL;
    int attached = 0;
    if ((*ctx->vm)->GetEnv(ctx->vm, (void **)&env, JNI_VERSION_1_6) == JNI_EDETACHED) {
        if ((*ctx->vm)->AttachCurrentThread(ctx->vm, &env, NULL) == JNI_OK) {
            attached = 1;
        } else {
            env = NULL;
        }
    }
    if (env != NULL) {
        jbyteArray array = NULL;
        if (status == 0 && data != NULL && length > 0) {
            array = (*env)->NewByteArray(env, (jsize)length);
            if (array != NULL) {
                (*env)->SetByteArrayRegion(env, array, 0, (jsize)length, (const jbyte *)data);
            }
        }
        (*env)->CallStaticVoidMethod(env, g_bridge, g_dispatch, ctx->task_id, (jint)status, array);
        if ((*env)->ExceptionCheck(env)) {
            (*env)->ExceptionClear(env);
        }
        if (array != NULL) {
            (*env)->DeleteLocalRef(env, array);
        }
    }
    if (attached) {
        (*ctx->vm)->DetachCurrentThread(ctx->vm);
    }
    if (data != NULL) {
        ctx->release(data);
    }
    free(ctx);
}

static jlong JNICALL bridge_load(JNIEnv *env, jclass cls, jstring jname, jstring jpath,
                                 jbyteArray jconfig) {
    (void)cls;
    if (jname == NULL || jpath == NULL) {
        throw_state(env, "插件名称和路径不能为空");
        return 0;
    }
    const char *name = (*env)->GetStringUTFChars(env, jname, NULL);
    const char *path = (*env)->GetStringUTFChars(env, jpath, NULL);
    jlong result = 0;
    plugin_handle *handle = NULL;
    uint8_t *config = NULL;

    if (name == NULL || path == NULL || strlen(name) > MAX_NAME) {
        throw_state(env, "插件名称或路径不合法");
        goto done;
    }
    jsize config_len = jconfig == NULL ? 0 : (*env)->GetArrayLength(env, jconfig);
    if (config_len < 0 || (uint32_t)config_len > MAX_CONFIG) {
        throw_state(env, "插件 config 超过 64KB");
        goto done;
    }
    if (config_len > 0) {
        config = (uint8_t *)malloc((size_t)config_len);
        if (config == NULL) {
            throw_state(env, "无法分配插件 config");
            goto done;
        }
        (*env)->GetByteArrayRegion(env, jconfig, 0, config_len, (jbyte *)config);
        if ((*env)->ExceptionCheck(env)) {
            goto done;
        }
    }

    void *lib = dlopen(path, RTLD_NOW | RTLD_LOCAL);
    if (lib == NULL) {
        char message[512];
        snprintf(message, sizeof(message), "无法加载插件 %s: %s", name, dlerror());
        throw_state(env, message);
        goto done;
    }
    /* 加载失败也不 dlclose：卸载运行中的原生代码风险更高。 */
    handle = (plugin_handle *)calloc(1, sizeof(plugin_handle));
    if (handle == NULL) {
        throw_state(env, "无法分配插件句柄");
        goto done;
    }
    handle->lib = lib;
    handle->describe = (berba_plugin_describe_fn)dlsym(lib, "berba_plugin_describe");
    handle->init = (berba_plugin_init_fn)dlsym(lib, "berba_plugin_init");
    handle->call = (berba_plugin_call_fn)dlsym(lib, "berba_plugin_call");
    handle->release = (berba_plugin_free_fn)dlsym(lib, "berba_plugin_free");
    if (handle->describe == NULL || handle->init == NULL || handle->call == NULL ||
        handle->release == NULL) {
        throw_state(env, "插件缺少必需的 C ABI 符号");
        goto done;
    }
    if (pthread_mutex_init(&handle->lock, NULL) != 0) {
        throw_state(env, "无法初始化插件锁");
        goto done;
    }

    berba_host host;
    host.abi_version = BERBA_PLUGIN_ABI_VERSION;
    host.context = handle;
    host.log = host_log;
    int32_t rc = handle->init(&host, config, (uint32_t)config_len);
    if (rc != 0) {
        throw_code(env, "插件初始化失败", rc);
        goto done;
    }
    result = (jlong)(intptr_t)handle;
    handle = NULL;

done:
    free(handle);
    free(config);
    if (name != NULL) {
        (*env)->ReleaseStringUTFChars(env, jname, name);
    }
    if (path != NULL) {
        (*env)->ReleaseStringUTFChars(env, jpath, path);
    }
    return result;
}

static jbyteArray JNICALL bridge_describe(JNIEnv *env, jclass cls, jlong raw) {
    (void)cls;
    plugin_handle *handle = handle_of(raw);
    if (handle == NULL) {
        throw_state(env, "插件句柄无效");
        return NULL;
    }
    const char *text = handle->describe();
    if (text == NULL) {
        return NULL;
    }
    size_t length = strlen(text);
    jbyteArray array = (*env)->NewByteArray(env, (jsize)length);
    if (array != NULL && length > 0) {
        (*env)->SetByteArrayRegion(env, array, 0, (jsize)length, (const jbyte *)text);
    }
    return array;
}

static jbyteArray JNICALL bridge_call(JNIEnv *env, jclass cls, jlong raw, jstring jmodule,
                                      jstring jfunction, jbyteArray jargs) {
    (void)cls;
    plugin_handle *handle = handle_of(raw);
    if (handle == NULL) {
        throw_state(env, "插件句柄无效");
        return NULL;
    }
    if (jmodule == NULL || jfunction == NULL) {
        throw_state(env, "模块名和函数名不能为空");
        return NULL;
    }
    const char *module = (*env)->GetStringUTFChars(env, jmodule, NULL);
    const char *function = (*env)->GetStringUTFChars(env, jfunction, NULL);
    uint8_t *args = NULL;
    jbyteArray result = NULL;
    if (module == NULL || function == NULL) {
        goto done;
    }
    jsize args_len = jargs == NULL ? 0 : (*env)->GetArrayLength(env, jargs);
    if (args_len < 0 || (uint32_t)args_len > MAX_ARGS) {
        throw_state(env, "插件参数超过 1MB");
        goto done;
    }
    if (args_len > 0) {
        args = (uint8_t *)malloc((size_t)args_len);
        if (args == NULL) {
            throw_state(env, "无法分配插件参数");
            goto done;
        }
        (*env)->GetByteArrayRegion(env, jargs, 0, args_len, (jbyte *)args);
        if ((*env)->ExceptionCheck(env)) {
            goto done;
        }
    }

    uint8_t *out = NULL;
    uint32_t out_len = 0;
    pthread_mutex_lock(&handle->lock);
    int32_t rc = handle->call(module, function, args, (uint32_t)args_len, &out, &out_len, NULL, NULL);
    pthread_mutex_unlock(&handle->lock);
    if (rc == BERBA_PLUGIN_ASYNC) {
        throw_state(env, "异步原生函数需要回调");
        goto done;
    }
    if (rc != BERBA_PLUGIN_OK) {
        throw_code(env, "原生插件调用失败", rc);
        goto done;
    }
    if (out_len > MAX_RESULT) {
        throw_state(env, "原生插件返回值超过 8MB");
        goto done;
    }
    if (out != NULL && out_len > 0) {
        result = (*env)->NewByteArray(env, (jsize)out_len);
        if (result != NULL) {
            (*env)->SetByteArrayRegion(env, result, 0, (jsize)out_len, (const jbyte *)out);
        }
    }
    if (out != NULL) {
        handle->release(out);
    }

done:
    free(args);
    if (module != NULL) {
        (*env)->ReleaseStringUTFChars(env, jmodule, module);
    }
    if (function != NULL) {
        (*env)->ReleaseStringUTFChars(env, jfunction, function);
    }
    return result;
}

static void JNICALL bridge_call_async(JNIEnv *env, jclass cls, jlong raw, jstring jmodule,
                                      jstring jfunction, jbyteArray jargs, jlong task_id) {
    (void)cls;
    plugin_handle *handle = handle_of(raw);
    if (handle == NULL) {
        throw_state(env, "插件句柄无效");
        return;
    }
    if (jmodule == NULL || jfunction == NULL) {
        throw_state(env, "模块名和函数名不能为空");
        return;
    }
    const char *module = (*env)->GetStringUTFChars(env, jmodule, NULL);
    const char *function = (*env)->GetStringUTFChars(env, jfunction, NULL);
    uint8_t *args = NULL;
    completion_ctx *ctx = NULL;
    if (module == NULL || function == NULL) {
        goto done;
    }
    jsize args_len = jargs == NULL ? 0 : (*env)->GetArrayLength(env, jargs);
    if (args_len < 0 || (uint32_t)args_len > MAX_ARGS) {
        throw_state(env, "插件参数超过 1MB");
        goto done;
    }
    if (args_len > 0) {
        args = (uint8_t *)malloc((size_t)args_len);
        if (args == NULL) {
            throw_state(env, "无法分配插件参数");
            goto done;
        }
        (*env)->GetByteArrayRegion(env, jargs, 0, args_len, (jbyte *)args);
        if ((*env)->ExceptionCheck(env)) {
            goto done;
        }
    }
    ctx = (completion_ctx *)calloc(1, sizeof(completion_ctx));
    if (ctx == NULL) {
        throw_state(env, "无法分配插件回调");
        goto done;
    }
    ctx->vm = g_vm;
    ctx->task_id = task_id;
    ctx->release = handle->release;

    uint8_t *out = NULL;
    uint32_t out_len = 0;
    pthread_mutex_lock(&handle->lock);
    int32_t rc = handle->call(module, function, args, (uint32_t)args_len, &out, &out_len,
                              complete_dispatch, ctx);
    pthread_mutex_unlock(&handle->lock);
    if (rc == BERBA_PLUGIN_ASYNC) {
        if (out != NULL) {
            handle->release(out);
        }
        ctx = NULL; /* 由插件线程的回调释放 */
    } else if (rc == BERBA_PLUGIN_OK) {
        complete_dispatch(ctx, 0, out, out_len);
        ctx = NULL;
    } else {
        if (out != NULL) {
            handle->release(out);
        }
        throw_code(env, "原生插件调用失败", rc);
    }

done:
    free(ctx);
    free(args);
    if (module != NULL) {
        (*env)->ReleaseStringUTFChars(env, jmodule, module);
    }
    if (function != NULL) {
        (*env)->ReleaseStringUTFChars(env, jfunction, function);
    }
}

static JNINativeMethod BRIDGE_METHODS[] = {
    {"nativeLoad", "(Ljava/lang/String;Ljava/lang/String;[B)J", (void *)bridge_load},
    {"nativeDescribe", "(J)[B", (void *)bridge_describe},
    {"nativeCall", "(JLjava/lang/String;Ljava/lang/String;[B)[B", (void *)bridge_call},
    {"nativeCallAsync", "(JLjava/lang/String;Ljava/lang/String;[BJ)V", (void *)bridge_call_async},
};

JNIEXPORT jint JNICALL JNI_OnLoad(JavaVM *vm, void *reserved) {
    (void)reserved;
    g_vm = vm;
    JNIEnv *env = NULL;
    if ((*vm)->GetEnv(vm, (void **)&env, JNI_VERSION_1_6) != JNI_OK) {
        return JNI_ERR;
    }
    jclass local = (*env)->FindClass(env, "com/berba/shell/NativePluginBridge");
    if (local == NULL) {
        return JNI_ERR;
    }
    g_bridge = (jclass)(*env)->NewGlobalRef(env, local);
    (*env)->DeleteLocalRef(env, local);
    if (g_bridge == NULL) {
        return JNI_ERR;
    }
    g_dispatch = (*env)->GetStaticMethodID(env, g_bridge, "dispatch", "(JI[B)V");
    if (g_dispatch == NULL) {
        return JNI_ERR;
    }
    if ((*env)->RegisterNatives(env, g_bridge, BRIDGE_METHODS,
                                (jint)(sizeof(BRIDGE_METHODS) / sizeof(BRIDGE_METHODS[0]))) != JNI_OK) {
        return JNI_ERR;
    }
    return JNI_VERSION_1_6;
}
