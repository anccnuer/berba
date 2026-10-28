package com.berba.shell;

import org.luaj.vm2.Globals;
import org.luaj.vm2.LuaTable;
import org.luaj.vm2.LuaValue;
import org.luaj.vm2.Varargs;
import org.luaj.vm2.lib.VarArgFunction;

/**
 * Lua 沙箱冒烟测试（纯 JVM，无需 Android）。由 build-shell 脚本调用。
 */
public final class LuaSandboxTest {
    private LuaSandboxTest() {
    }

    public static void main(String[] args) {
        Globals g = LuaSandbox.create();

        check(g.get("table").istable(), "table 库未加载");
        check(g.get("string").istable(), "string 库未加载");
        check(g.get("math").istable(), "math 库未加载");
        check(g.get("coroutine").istable(), "coroutine 库未加载");
        check(g.get("bit32").istable(), "bit32 库未加载");
        check(g.get("package").isnil(), "package 不应暴露");
        check(g.get("load").isnil(), "load 不应暴露");
        check(g.get("dofile").isnil(), "dofile 不应暴露");
        check(g.get("loadfile").isnil(), "loadfile 不应暴露");
        check(g.get("luajava").isnil(), "luajava 不应暴露");

        LuaTable registered = new LuaTable();
        LuaTable bridge = new LuaTable();
        bridge.set("register", new VarArgFunction() {
            @Override
            public Varargs invoke(Varargs a) {
                registered.set(a.checkjstring(1), a.arg(2));
                return LuaValue.NIL;
            }
        });
        g.set("bridge", bridge);

        g.load("bridge.register('add', function(req) return { sum = (req.a or 0) + (req.b or 0) } end)",
                "@test").call();
        LuaValue fn = registered.get("add");
        check(fn.isfunction(), "bridge.register 未生效");

        LuaTable req = new LuaTable();
        req.set("a", 2);
        req.set("b", 40);
        check(fn.invoke(req).arg1().get("sum").toint() == 42, "桥接函数计算结果错误");

        System.out.println("LuaSandbox OK");
    }

    private static void check(boolean ok, String message) {
        if (!ok) {
            throw new AssertionError(message);
        }
    }
}
