// Approach 2: Run PGlite's CJS bundle inside goja (pure-Go JS engine).
//
// PGlite's pglite.cjs is Emscripten-generated JS glue that creates a
// WebAssembly.Memory/Table/Global and calls WebAssembly.instantiate to load
// the .wasm. We implement WebAssembly.* in Go (backed by wazero) and inject
// them into goja so the Emscripten glue can execute.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/dop251/goja_nodejs/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// wasmInstance wraps a wazero module instance and the compiled module.
type wasmInstance struct {
	rt       wazero.Runtime
	mod      api.Module
	compiled wazero.CompiledModule
	mu       sync.Mutex
}

// setupWebAssembly injects a WebAssembly global into the goja runtime.
// This bridges JS WebAssembly.* calls to wazero.
func setupWebAssembly(vm *goja.Runtime, loop *eventloop.EventLoop) {
	wasmObj := vm.NewObject()

	// WebAssembly.Memory — we implement as a Go-backed object with a .buffer
	memCtor := func(call goja.ConstructorCall) *goja.Object {
		opts := call.Argument(0).ToObject(vm)
		initial := opts.Get("initial").ToInteger()
		maximum := opts.Get("maximum").ToInteger()
		if maximum == 0 {
			maximum = initial
		}
		pageSize := int64(65536)
		size := initial * pageSize

		backing := make([]byte, size, int64(maximum)*pageSize)

		memObj := vm.NewObject()
		buf := vm.NewArrayBuffer(backing)
		memObj.Set("buffer", buf)
		memObj.Set("grow", func(pages int64) int64 {
			oldSize := int64(len(backing))
			newSize := oldSize + pages*pageSize
			if newSize > int64(maximum)*pageSize {
				panic(vm.NewTypeError("memory.grow: out of bounds"))
			}
			newBacking := make([]byte, newSize)
			copy(newBacking, backing)
			backing = newBacking
			buf = vm.NewArrayBuffer(backing)
			memObj.Set("buffer", buf)
			return oldSize / pageSize
		})
		_ = initial
		return memObj
	}
	vm.Set("__wasmMemoryCtor", memCtor)
	vm.RunString(`
		function WasmMemory(opts) {
			return __wasmMemoryCtor.call(this, opts);
		}
	`)
	wasmMemory, _ := vm.RunString(`WasmMemory`)
	wasmObj.Set("Memory", wasmMemory)

	// WebAssembly.Table — a function reference table (stub)
	tableCtor := func(call goja.ConstructorCall) *goja.Object {
		opts := call.Argument(0).ToObject(vm)
		initial := opts.Get("initial").ToInteger()

		tableObj := vm.NewObject()
		entries := make([]goja.Value, initial)
		for i := range entries {
			entries[i] = goja.Null()
		}
		tableObj.Set("length", initial)
		tableObj.Set("get", func(i int64) goja.Value {
			if i < int64(len(entries)) {
				return entries[i]
			}
			return goja.Null()
		})
		tableObj.Set("set", func(i int64, v goja.Value) {
			for int64(len(entries)) <= i {
				entries = append(entries, goja.Null())
			}
			entries[i] = v
		})
		tableObj.Set("grow", func(n int64) int64 {
			old := int64(len(entries))
			for i := int64(0); i < n; i++ {
				entries = append(entries, goja.Null())
			}
			tableObj.Set("length", int64(len(entries)))
			return old
		})
		return tableObj
	}
	vm.Set("__wasmTableCtor", tableCtor)
	vm.RunString(`function WasmTable(opts) { return __wasmTableCtor.call(this, opts); }`)
	wasmTable, _ := vm.RunString(`WasmTable`)
	wasmObj.Set("Table", wasmTable)

	// WebAssembly.Global — a mutable/immutable global value box
	globalCtor := func(call goja.ConstructorCall) *goja.Object {
		descriptor := call.Argument(0).ToObject(vm)
		initialVal := call.Argument(1)
		mutable := descriptor.Get("mutable")
		isMutable := mutable != nil && mutable != goja.Undefined() && mutable.ToBoolean()

		var val goja.Value
		if initialVal == goja.Undefined() || initialVal == nil {
			val = vm.ToValue(int64(0))
		} else {
			val = initialVal
		}

		globalObj := vm.NewObject()
		globalObj.Set("value", val)
		_ = isMutable
		return globalObj
	}
	vm.Set("__wasmGlobalCtor", globalCtor)
	vm.RunString(`function WasmGlobal(desc, val) { return __wasmGlobalCtor.call(this, desc, val); }`)
	wasmGlobal, _ := vm.RunString(`WasmGlobal`)
	wasmObj.Set("Global", wasmGlobal)

	// WebAssembly.Module — just wraps binary bytes
	moduleCtor := func(call goja.ConstructorCall) *goja.Object {
		bytes := call.Argument(0)
		fmt.Printf("[WebAssembly.Module] constructor called, arg type=%T\n", bytes.Export())
		modObj := vm.NewObject()
		modObj.Set("__bytes", bytes)
		return modObj
	}
	vm.Set("__wasmModuleCtor", moduleCtor)
	vm.RunString(`function WasmModule(bytes) { return __wasmModuleCtor.call(this, bytes); }`)
	wasmModule, _ := vm.RunString(`WasmModule`)

	// WebAssembly.Module.customSections — PGlite uses this to read dylink section
	vm.Set("__wasmModuleCustomSections", func(mod goja.Value, name string) goja.Value {
		// Return empty array — PGlite uses this to check if it's a dylink module
		return vm.NewArray()
	})
	vm.RunString(`WasmModule.customSections = __wasmModuleCustomSections`)
	wasmObj.Set("Module", wasmModule)

	// WebAssembly.Instance — this is where we need to actually run the WASM.
	// We'll instantiate synchronously using wazero.
	instanceCtor := func(call goja.ConstructorCall) *goja.Object {
		fmt.Println("[WebAssembly.Instance] constructor called")
		modVal := call.Argument(0).ToObject(vm)
		importsVal := call.Argument(1)

		bytesVal := modVal.Get("__bytes")
		_ = importsVal // we'll need to bridge these back

		// Convert JS Uint8Array/ArrayBuffer to Go bytes
		var wasmBytes []byte
		if ab, ok := bytesVal.Export().(goja.ArrayBuffer); ok {
			wasmBytes = ab.Bytes()
		} else {
			// Try exporting as []byte
			exported := bytesVal.Export()
			switch v := exported.(type) {
			case []byte:
				wasmBytes = v
			case goja.ArrayBuffer:
				wasmBytes = v.Bytes()
			default:
				panic(fmt.Sprintf("WebAssembly.Instance: cannot get bytes from %T", exported))
			}
		}

		fmt.Printf("[WebAssembly.Instance] instantiating %d bytes of WASM\n", len(wasmBytes))

		// Try instantiation with wazero
		ctx := context.Background()
		rt := wazero.NewRuntime(ctx)

		compiled, err := rt.CompileModule(ctx, wasmBytes)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("WebAssembly.Instance compile: %w", err)))
		}

		// Print what this module needs
		for _, fn := range compiled.ImportedFunctions() {
			m, n, _ := fn.Import()
			fmt.Printf("  needs import: %s.%s\n", m, n)
		}

		_ = compiled
		rt.Close(ctx)

		// Return a stub exports object
		exportsObj := vm.NewObject()
		instObj := vm.NewObject()
		instObj.Set("exports", exportsObj)
		return instObj
	}
	vm.Set("__wasmInstanceCtor", instanceCtor)
	vm.RunString(`function WasmInstance(mod, imports) { return __wasmInstanceCtor.call(this, mod, imports); }`)
	wasmInstance, _ := vm.RunString(`WasmInstance`)
	wasmObj.Set("Instance", wasmInstance)

	// WebAssembly.instantiate(bytes, imports) -> Promise<{module, instance}>
	// We resolve synchronously via loop.RunOnLoop so that the resolve fires on the
	// event loop goroutine (goja is not goroutine-safe).
	wasmObj.Set("instantiate", func(call goja.FunctionCall) goja.Value {
		fmt.Println("[WebAssembly.instantiate] called")
		bytesArg := call.Argument(0)
		importsArg := call.Argument(1)
		_ = importsArg

		promise, resolve, reject := vm.NewPromise()

		var wasmBytes []byte
		exported := bytesArg.Export()
		switch v := exported.(type) {
		case goja.ArrayBuffer:
			wasmBytes = v.Bytes()
		case []byte:
			wasmBytes = v
		case map[string]interface{}:
			// PGlite passes { buffer: ArrayBuffer } (from fs.readFile stub)
			if buf, ok := v["buffer"]; ok {
				if ab, ok := buf.(goja.ArrayBuffer); ok {
					wasmBytes = ab.Bytes()
				}
			}
			if wasmBytes == nil {
				loop.RunOnLoop(func(_ *goja.Runtime) {
					_ = reject(fmt.Sprintf("instantiate: map arg missing buffer field, keys=%v", func() []string {
						keys := make([]string, 0, len(v))
						for k := range v {
							keys = append(keys, k)
						}
						return keys
					}()))
				})
				return vm.ToValue(promise)
			}
		default:
			// Try .buffer property from JS object
			if obj, ok := bytesArg.Export().(*goja.Object); ok {
				_ = obj
			}
			if jsObj := bytesArg.ToObject(vm); jsObj != nil {
				if bufVal := jsObj.Get("buffer"); bufVal != nil && bufVal != goja.Undefined() {
					if ab, ok := bufVal.Export().(goja.ArrayBuffer); ok {
						wasmBytes = ab.Bytes()
					}
				}
			}
			if wasmBytes == nil {
				fmt.Printf("[WebAssembly.instantiate] unexpected type %T\n", exported)
				loop.RunOnLoop(func(_ *goja.Runtime) {
					_ = reject(fmt.Sprintf("instantiate: unexpected bytes type %T", exported))
				})
				return vm.ToValue(promise)
			}
		}

		fmt.Printf("[WebAssembly.instantiate] got %d WASM bytes, spawning goroutine\n", len(wasmBytes))

		// Compile synchronously in a goroutine, then resolve back on the loop thread.
		wasmBytesCopy := make([]byte, len(wasmBytes))
		copy(wasmBytesCopy, wasmBytes)

		go func() {
			fmt.Println("[WebAssembly.instantiate] goroutine started, compiling...")
			ctx := context.Background()
			rt := wazero.NewRuntime(ctx)
			defer rt.Close(ctx)

			compiled, err := rt.CompileModule(ctx, wasmBytesCopy)
			if err != nil {
				fmt.Printf("[WebAssembly.instantiate] compile failed: %v\n", err)
				loop.RunOnLoop(func(_ *goja.Runtime) {
					_ = reject(fmt.Sprintf("compile failed: %v", err))
				})
				return
			}
			defer compiled.Close(ctx)

			nExports := len(compiled.ExportedFunctions())
			fmt.Printf("[WebAssembly.instantiate] compiled OK, %d exports — scheduling resolve\n", nExports)

			loop.RunOnLoop(func(rt2 *goja.Runtime) {
				fmt.Println("[WebAssembly.instantiate] resolving promise on loop thread")
				modObj := rt2.NewObject()
				modObj.Set("__bytes", rt2.NewArrayBuffer(wasmBytesCopy))
				instObj := rt2.NewObject()
				exportsObj := rt2.NewObject()
				instObj.Set("exports", exportsObj)

				result := rt2.NewObject()
				result.Set("module", modObj)
				result.Set("instance", instObj)
				_ = resolve(result)
			})
		}()

		return vm.ToValue(promise)
	})

	// WebAssembly.instantiateStreaming — not supported, fall back to instantiate
	wasmObj.Set("instantiateStreaming", goja.Undefined())

	// WebAssembly.Function — used by Emscripten for type-checked calls
	wasmObj.Set("Function", goja.Undefined())

	// WebAssembly.RuntimeError
	vm.RunString(`
		function WasmRuntimeError(msg) { Error.call(this, msg); this.message = msg; }
		WasmRuntimeError.prototype = Object.create(Error.prototype);
	`)
	runtimeError, _ := vm.RunString(`WasmRuntimeError`)
	wasmObj.Set("RuntimeError", runtimeError)

	vm.Set("WebAssembly", wasmObj)
}

// setupNodeShims sets up minimal Node.js-like globals that PGlite's CJS bundle needs.
func setupNodeShims(vm *goja.Runtime, wasmData []byte, wasmBytes []byte) {
	// process
	processObj := vm.NewObject()
	versionsObj := vm.NewObject()
	versionsObj.Set("node", "18.0.0")
	processObj.Set("versions", versionsObj)
	processObj.Set("platform", "linux")
	processObj.Set("env", vm.NewObject())
	processObj.Set("argv", vm.NewArray(0)) // empty argv so .slice(2) returns []
	processObj.Set("exit", func(code int) { os.Exit(code) })
	// process.binding("constants") — used by NODEFS for file system flag constants
	processObj.Set("binding", func(name string) goja.Value {
		if name == "constants" {
			consts := vm.NewObject()
			fs := vm.NewObject()
			// POSIX file open flags
			fs.Set("O_RDONLY", 0)
			fs.Set("O_WRONLY", 1)
			fs.Set("O_RDWR", 2)
			fs.Set("O_CREAT", 64)
			fs.Set("O_EXCL", 128)
			fs.Set("O_NOCTTY", 256)
			fs.Set("O_TRUNC", 512)
			fs.Set("O_APPEND", 1024)
			fs.Set("O_SYNC", 4096)
			fs.Set("O_NOFOLLOW", 131072)
			consts.Set("fs", fs)
			return consts
		}
		return vm.NewObject()
	})
	processObj.Set("hrtime", func() goja.Value {
		bigintObj := vm.NewObject()
		bigintObj.Set("__val", int64(0))
		return bigintObj
	})
	processObj.Set("nextTick", func(fn goja.Callable, args ...goja.Value) {
		// Schedule on next tick — simplified
		go func() { fn(goja.Undefined(), args...) }()
	})
	vm.Set("process", processObj)

	// Buffer — minimal
	vm.RunString(`
		var Buffer = {
			from: function(data, encoding) {
				if (typeof data === 'string') {
					var bytes = [];
					for (var i = 0; i < data.length; i++) bytes.push(data.charCodeAt(i) & 0xff);
					return new Uint8Array(bytes);
				}
				return new Uint8Array(data);
			},
			alloc: function(size, fill) {
				var buf = new Uint8Array(size);
				if (fill !== undefined) buf.fill(fill);
				return buf;
			},
			isBuffer: function(obj) { return obj instanceof Uint8Array; },
			concat: function(bufs) {
				var len = bufs.reduce(function(a,b){ return a + b.length; }, 0);
				var out = new Uint8Array(len);
				var pos = 0;
				bufs.forEach(function(b){ out.set(b, pos); pos += b.length; });
				return out;
			}
		};
	`)

	// __filename
	vm.Set("__filename", "/pglite/pglite.cjs")

	// fetch — returns the pglite.data file when requested
	vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		fmt.Printf("[fetch] called: %s\n", url)

		promise, resolve, _ := vm.NewPromise()

		var data []byte
		// Match any URL that ends in pglite.data or pglite.wasm
		if strings.HasSuffix(url, "pglite.data") {
			fmt.Printf("[fetch] serving pglite.data (%d bytes)\n", len(wasmData))
			data = wasmData
		} else if strings.HasSuffix(url, "pglite.wasm") {
			fmt.Printf("[fetch] serving pglite.wasm (%d bytes)\n", len(wasmBytes))
			data = wasmBytes
		} else {
			fmt.Printf("[fetch] unknown URL, returning empty: %s\n", url)
			data = []byte{}
		}

		respObj := vm.NewObject()
		respObj.Set("ok", true)
		respObj.Set("status", 200)
		respObj.Set("arrayBuffer", func() goja.Value {
			p2, res2, _ := vm.NewPromise()
			buf := vm.NewArrayBuffer(data)
			_ = res2(buf)
			return vm.ToValue(p2)
		})
		respObj.Set("text", func() goja.Value {
			p2, res2, _ := vm.NewPromise()
			_ = res2(string(data))
			return vm.ToValue(p2)
		})
		_ = resolve(respObj)
		return vm.ToValue(promise)
	})

	// Worker — stub (PGlite uses workers for threading, we'll disable)
	vm.RunString(`
		function Worker() { throw new Error('Worker not supported in this environment'); }
	`)

	// crypto
	vm.Set("crypto", vm.NewObject())
	cryptoMod := vm.Get("crypto").ToObject(vm)
	cryptoMod.Set("getRandomValues", func(buf goja.Value) goja.Value {
		// Fill with pseudo-random bytes
		if ab, ok := buf.Export().(goja.ArrayBuffer); ok {
			b := ab.Bytes()
			for i := range b {
				b[i] = byte(i * 7 % 251)
			}
		}
		return buf
	})

	// Inject the data/wasm buffers as JS globals so __nodeImport's readFile stub
	// can return them without needing the Go byte slices in scope there.
	vm.Set("__pgliteDataBuf", vm.NewArrayBuffer(wasmData))
	vm.Set("__pgliteWasmBuf", vm.NewArrayBuffer(wasmBytes))
}

func runApproach2() error {
	// Use the eventloop so Promise resolution and setTimeout work.
	// We use Start()/Stop() rather than Run() so the loop stays alive while
	// our goroutine (inside WebAssembly.instantiate) does its work.
	loop := eventloop.NewEventLoop()

	// done channel receives the final result from inside the event loop.
	done := make(chan error, 1)

	loop.Start()

	loop.RunOnLoop(func(vm *goja.Runtime) {
		setupErr := runApproach2Inner(vm, loop, done)
		if setupErr != nil {
			done <- setupErr
		}
	})

	// Wait for the promise chain to complete (success or error).
	err := <-done
	loop.Stop()
	return err
}

func runApproach2Inner(vm *goja.Runtime, loop *eventloop.EventLoop, done chan<- error) error {
	// Set up require registry with Node.js built-ins
	registry := require.NewRegistry(require.WithGlobalFolders("."))
	registry.Enable(vm)

	// Set up WebAssembly bridge
	setupWebAssembly(vm, loop)

	// Set up Node.js shims
	setupNodeShims(vm, pgliteData, pgliteWasm)

	// TextEncoder / TextDecoder — used by PGlite for string<->byte conversion
	vm.RunString(`
		function TextEncoder() {}
		TextEncoder.prototype.encodeInto = function(str, dest) {
			var encoded = this.encode(str);
			var written = Math.min(encoded.length, dest.length);
			for (var i = 0; i < written; i++) dest[i] = encoded[i];
			return { read: str.length, written: written };
		};
		TextEncoder.prototype.encode = function(str) {
			var bytes = [];
			for (var i = 0; i < str.length; i++) {
				var code = str.charCodeAt(i);
				if (code < 0x80) { bytes.push(code); }
				else if (code < 0x800) { bytes.push(0xC0|(code>>6)); bytes.push(0x80|(code&0x3F)); }
				else { bytes.push(0xE0|(code>>12)); bytes.push(0x80|((code>>6)&0x3F)); bytes.push(0x80|(code&0x3F)); }
			}
			return new Uint8Array(bytes);
		};
		function TextDecoder(enc) { this.encoding = enc || 'utf-8'; }
		TextDecoder.prototype.decode = function(buf) {
			if (!buf) return '';
			var bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
			var str = '';
			for (var i = 0; i < bytes.length; i++) {
				var b = bytes[i];
				if (b < 0x80) { str += String.fromCharCode(b); }
				else if ((b & 0xE0) === 0xC0) { str += String.fromCharCode(((b&0x1F)<<6)|(bytes[++i]&0x3F)); }
				else { str += String.fromCharCode(((b&0x0F)<<12)|((bytes[++i]&0x3F)<<6)|(bytes[++i]&0x3F)); }
			}
			return str;
		};
	`)

	// URL — used by PGlite to resolve the script's own URL (new URL(`file:${__filename}`))
	vm.RunString(`
		function URL(href, base) {
			if (base && !href.startsWith('http') && !href.startsWith('file:') && !href.startsWith('data:')) {
				var baseStr = typeof base === 'string' ? base : base.href;
				var dir = baseStr.replace(/\/[^\/]*$/, '');
				this.href = dir + '/' + href;
			} else {
				this.href = href;
			}
			this.toString = function() { return this.href; };
		}
	`)

	// __nodeImport: synchronous stub for all `await import(X)` calls.
	// NOTE: ko() (the pglite.data loader) runs in "Node mode" because process.versions.node
	// is set. It calls: (await import("fs/promises")).readFile(urlObject)
	// We intercept readFile and return the actual pglite.data bytes when the URL ends in
	// "pglite.data", and the pglite.wasm bytes for "pglite.wasm".
	// (buffers injected by setupNodeShims via __pgliteDataBuf / __pgliteWasmBuf)
	vm.RunString(`
		function __nodeImport(mod) {
			if (mod === 'module') {
				return { createRequire: function(url) { return require; } };
			}
			if (mod === 'util') {
				return { promisify: function(fn) { return function() { return Promise.resolve(new Uint8Array(0)); }; } };
			}
			if (mod === 'zlib') {
				return {
					gzip: function(data, cb) { cb && cb(null, data); },
					gunzip: function(data, cb) { cb && cb(null, data); }
				};
			}
			if (mod === 'fs' || mod === 'fs/promises') {
				return {
					readFile: function(p) {
						var href = (p && p.href) ? p.href : String(p);
						if (href.indexOf('pglite.data') !== -1) {
							// Return a Uint8Array so callers get .buffer (ArrayBuffer) and .subarray()
							return Promise.resolve(new Uint8Array(__pgliteDataBuf));
						}
						if (href.indexOf('pglite.wasm') !== -1) {
							return Promise.resolve(new Uint8Array(__pgliteWasmBuf));
						}
						return Promise.resolve(new Uint8Array(0));
					},
					writeFile: function(p, d) { return Promise.resolve(); },
					existsSync: function() { return false; },
					mkdirSync: function() {},
					createWriteStream: function() { return { write: function(){}, end: function(){} }; }
				};
			}
			if (mod === 'stream' || mod === 'stream/promises') {
				return { Writable: function(){}, pipeline: function() { return Promise.resolve(); } };
			}
			return {};
		}
	`)

	// Set up a module system shim (PGlite CJS uses module.exports)
	vm.RunString(`
		var module = { exports: {} };
		var exports = module.exports;
		var require = function(mod) {
			if (mod === 'crypto') return crypto;
			if (mod === 'fs') return {
				readFileSync: function(path) { return new Uint8Array(0); },
				existsSync: function() { return false; }
			};
			if (mod === 'path') return {
				join: function() { return Array.prototype.join.call(arguments, '/'); },
				dirname: function(p) { return p.split('/').slice(0,-1).join('/'); },
				resolve: function(p) { return p; }
			};
			if (mod === 'url') return {
				pathToFileURL: function(p) { return { href: 'file://' + p }; },
				fileURLToPath: function(u) {
					var s = (u && u.href) ? u.href : String(u);
					return s.replace(/^file:\/\//, '');
				}
			};
			if (mod === 'ws') return function(){}; // WebSocket stub
			throw new Error('Cannot require: ' + mod);
		};
	`)

	// Load index.cjs — the full PGlite bundle (Emscripten glue + PGlite class).
	// pglite.cjs is only the raw Emscripten module; index.cjs includes the PGlite
	// class, query API, and all TypeScript logic.
	//
	// goja does not support dynamic import(). index.cjs has 12 `await import(X)` calls
	// for Node built-ins (module, util, zlib, fs, stream). Replace all of them with
	// a synchronous stub lookup via __nodeImport(X). The stub returns an object with
	// the properties each call destructures — we only need gzip, gunzip, createRequire
	// since the fs/stream paths are only hit when writing to disk (not in-memory mode).
	dynamicImportRe := strings.NewReplacer() // unused placeholder
	_ = dynamicImportRe
	// Replace all `await import(X)` with `__nodeImport(X)` using a simple string approach.
	// There are only 7 distinct modules used, so a direct ReplaceAll loop is fine.
	cjsCode := string(indexCJS)
	for _, mod := range []string{"module", "util", "zlib", "fs/promises", "fs", "stream/promises", "stream"} {
		cjsCode = strings.ReplaceAll(cjsCode,
			`await import("`+mod+`")`,
			`__nodeImport("`+mod+`")`)
		cjsCode = strings.ReplaceAll(cjsCode,
			"await import('"+mod+"')",
			"__nodeImport('"+mod+"')")
	}
	// Strip the sourceMappingURL comment — goja tries to load it and fails with a SyntaxError.
	cjsCode = strings.ReplaceAll(cjsCode, "//# sourceMappingURL=index.cjs.map", "")
	fmt.Printf("[goja] Loading index.cjs (%d bytes, patched %d dynamic imports)...\n",
		len(cjsCode), strings.Count(string(indexCJS), "await import("))

	_, err := vm.RunScript("index.cjs", cjsCode)
	if err != nil {
		return fmt.Errorf("index.cjs load failed: %w", err)
	}

	fmt.Println("[goja] index.cjs loaded. Trying to create PGlite instance...")

	// Get the PGlite exports
	_, pgliteErr := vm.RunString(`
		var PGlite = module.exports.PGlite || module.exports.default;
	`)
	if pgliteErr != nil {
		return fmt.Errorf("getting PGlite export: %w", pgliteErr)
	}
	pgliteType, _ := vm.RunString(`typeof PGlite`)
	fmt.Printf("[goja] PGlite type: %v\n", pgliteType)

	// PGlite.create() is the async factory — returns a Promise<PGlite>.
	// We chain all SQL work in .then() callbacks so everything runs on the event loop.
	// Results are sent back to Go through the done channel.
	fmt.Println("[goja] Calling PGlite.create() for in-memory instance...")

	// Catch unhandled promise rejections so we can diagnose failures.
	vm.SetPromiseRejectionTracker(func(p *goja.Promise, operation goja.PromiseRejectionOperation) {
		if operation == goja.PromiseRejectionReject {
			reason := p.Result()
			msg := "unhandled promise rejection"
			if reason != nil {
				msg = reason.String()
				// goja JS exceptions are *goja.Exception when thrown from JS code
				if ex, ok := reason.Export().(error); ok {
					msg += "\nSTACK: " + ex.Error()
				} else if obj, ok := reason.(*goja.Object); ok {
					if stack := obj.Get("stack"); stack != nil && stack != goja.Undefined() {
						msg += "\nSTACK: " + stack.String()
					}
				}
			}
			fmt.Printf("[goja] UNHANDLED REJECTION: %s\n", msg)
			select {
			case done <- fmt.Errorf("unhandled rejection: %s", msg):
			default:
			}
		}
	})

	// Expose a Go callback that the JS promise chain calls when done.
	vm.Set("__goReportSuccess", func(result string) {
		fmt.Printf("[goja] Query result: %s\n", result)
		done <- nil
	})
	vm.Set("__goReportError", func(msg string) {
		done <- fmt.Errorf("%s", msg)
	})

	// Patch Uint8Array.prototype.subarray if missing (goja may not implement it)
	vm.RunString(`
		if (!Uint8Array.prototype.subarray) {
			Uint8Array.prototype.subarray = function(begin, end) {
				if (end === undefined) end = this.length;
				if (begin < 0) begin = Math.max(0, this.length + begin);
				if (end < 0) end = Math.max(0, this.length + end);
				var len = Math.max(0, end - begin);
				var out = new Uint8Array(len);
				for (var i = 0; i < len; i++) out[i] = this[begin + i];
				return out;
			};
			console.log('[js] Patched Uint8Array.prototype.subarray');
		} else {
			console.log('[js] Uint8Array.prototype.subarray already exists');
		}
	`)

	_, initErr := vm.RunString(`
		console.log('[js] About to call PGlite.create()...');
		PGlite.create().then(function(db) {
			console.log('[js] PGlite.create() resolved!');
			return db.exec('CREATE TABLE deployments (id SERIAL, name TEXT, sha TEXT, deployed_at TEXT)')
				.then(function() {
					return db.exec("INSERT INTO deployments (name, sha, deployed_at) VALUES ('gomanan', 'abc123', '2026-03-19T00:00:00Z')");
				})
				.then(function() {
					return db.query('SELECT * FROM deployments');
				})
				.then(function(result) {
					__goReportSuccess(JSON.stringify(result.rows));
				});
		}).catch(function(e) {
			console.log('[js] PGlite.create() rejected: ' + String(e));
			var msg = String(e) + (e && e.stack ? '\nSTACK: ' + e.stack : '');
			__goReportError(msg);
		});
		console.log('[js] PGlite.create() called, waiting for resolution...');
	`)
	if initErr != nil {
		return fmt.Errorf("PGlite.create() setup failed: %w", initErr)
	}
	return nil
}

