// Package luaruntime 内嵌浏览器端 Lua 运行时（fengari + 桥适配层），仅供 berba dev 使用。
//
// assets/fengari-web.js 取自 npm 包 fengari-web@0.1.4 的 dist/fengari-web.js
// （UMD 生产构建，webpack -p，加载后暴露全局 window.fengari）。
//
//	sha256 fbea0ee4b2e38840f96b03eb7ee162ae391127df26fccd000a2b8756535c8186
//
// assets/LICENSE 为同版本 MIT 许可证原文（Copyright © 2017 Benoit Giannangeli,
// Copyright © 2017-2018 Daurnimator）。
// assets/runtime.js 为 berba 自有代码：用 fengari 核心 API 模拟 LuaJ 环境下的 berba API。
package luaruntime

import (
	_ "embed"
)

// Fengari 是 fengari-web 的 UMD 构建，浏览器加载后提供 window.fengari。
//
//go:embed assets/fengari-web.js
var Fengari []byte

// Runtime 是 berba 的浏览器 Lua 适配层，暴露 window.__berbaLua。
//
//go:embed assets/runtime.js
var Runtime []byte

// License 是 fengari-web 的 MIT 许可证原文。
//
//go:embed assets/LICENSE
var License []byte
