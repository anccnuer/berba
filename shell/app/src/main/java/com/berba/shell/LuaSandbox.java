package com.berba.shell;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.compiler.LuaC;
import org.luaj.vm2.lib.Bit32Lib;
import org.luaj.vm2.lib.CoroutineLib;
import org.luaj.vm2.lib.StringLib;
import org.luaj.vm2.lib.TableLib;
import org.luaj.vm2.lib.jse.JseBaseLib;
import org.luaj.vm2.lib.jse.JseMathLib;

/**
 * 构造 Lua 沙箱基础环境。不依赖 Android，便于在 JVM 上做冒烟测试。
 */
final class LuaSandbox {
    private LuaSandbox() {
    }

    static Globals create() {
        Globals g = new Globals();
        // LuaJ 的 Table/String/Coroutine/Bit32 库加载时会写入 package.loaded；
        // 沙箱不暴露 package，因此加载期间给一个临时表，加载完立即移除。
        LuaTable bootstrap = new LuaTable();
        bootstrap.set("loaded", new LuaTable());
        g.set("package", bootstrap);
        g.load(new JseBaseLib());
        g.load(new TableLib());
        g.load(new StringLib());
        g.load(new JseMathLib());
        g.load(new CoroutineLib());
        g.load(new Bit32Lib());
        LuaC.install(g);

        g.set("load", LuaValue.NIL);
        g.set("dofile", LuaValue.NIL);
        g.set("loadfile", LuaValue.NIL);
        g.set("package", LuaValue.NIL);
        g.set("luajava", LuaValue.NIL);
        return g;
    }
}
