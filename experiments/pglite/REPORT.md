# PGlite Embedding Experiment — Report

**Date:** 2026-03-19  
**Goal:** Embed PGlite (Electric SQL WASM PostgreSQL) inside a Go binary; open an in-memory DB, run `CREATE TABLE`, `INSERT`, `SELECT *`, print results.

---

## Results Summary

| Approach | Outcome | Root cause |
|----------|---------|------------|
| 1. wazero + raw WASM | BLOCKED | `env` module needs to be simultaneously a WASM module (memory/table/globals) and a host function module — architecturally impossible in wazero |
| 2. goja + PGlite CJS | BLOCKED | Can compile the WASM and bridge WebAssembly.instantiate, but the returned exports are stubs with no real functions; calling into them hits missing `subarray` on undefined |
| 3. Node.js subprocess | SUCCESS | Writes embedded assets to tmpdir, spawns Node.js, communicates via JSON-lines stdout |

**Working output (Approach 3):**
```
row[0]: id=1 name=gomanan sha=abc123 deployed_at=2026-03-19T00:00:00Z
```

---

## Approach 1: wazero + raw WASM

**What was tried:** Instantiate `pglite.wasm` directly in wazero. Provide all 117 `env` imports (invoke_* + syscall stubs) plus WASI.

**Exact failure:**
```
module[env] has already been instantiated
```

**Root cause:** PGlite is compiled with Emscripten as a relocatable side-module (`-sSIDE_MODULE`). It imports from a single module named `"env"`:
- Memory (`env.memory`) — must come from a WASM module (wazero `HostModuleBuilder` cannot export memory)
- Function table (`env.__indirect_function_table`) — same problem
- GOT globals (`env.__memory_base`, `env.__stack_pointer`, `env.__table_base`) — same
- 117 host functions (`invoke_*`, syscalls, emscripten runtime) — must come from a host function module

wazero cannot instantiate two modules with the same name. It also cannot make a single module that exports both memory/table/globals AND provides host functions. This is a hard architectural constraint — not a missing feature that could be worked around.

**Workaround explored:** Hand-craft a binary WASM module named `"env"` that exports memory + table + globals, then use wazero's built-in `emscripten.InstantiateForModule` for invoke_* functions. This also fails because `InstantiateForModule` tries to create a second module named `"env"`.

**Conclusion:** Cannot work with wazero.

---

## Approach 2: goja (pure Go JS engine) + PGlite CJS

**What was tried:** Run `index.cjs` (573KB full PGlite bundle) inside goja. Implement `WebAssembly.*` in Go (backed by wazero for compilation). Provide Node.js shims for `process`, `Buffer`, `fetch`, `crypto`, `TextEncoder`, `URL`, `require()`, `__nodeImport()`.

**Errors encountered and fixed (in order):**

1. `SyntaxError: Unexpected reserved word` — goja doesn't support dynamic `import()`. Fixed: replace all `await import("X")` with `__nodeImport("X")` (12 occurrences: module, util, zlib, fs/promises, fs, stream/promises, stream).
2. `SyntaxError: Could not load source map` — goja tries to load `index.cjs.map`. Fixed: strip `//# sourceMappingURL=index.cjs.map`.
3. `TypeError: Value is not a constructor` — was loading `pglite.cjs` (low-level glue only); switched to `index.cjs` (full bundle with PGlite class).
4. `ReferenceError: URL is not defined` — added URL constructor shim.
5. `TypeError: Object has no member 'encodeInto'` — added `TextEncoder.prototype.encodeInto`.
6. `TypeError: Object has no member 'fileURLToPath'` — `require("module").createRequire()` returned a function that returned `{}` for all modules. Fixed: `createRequire` returns the actual `require` stub.
7. `TypeError: Cannot read property 'slice' of undefined` — `process.argv` was missing. Fixed: added `process.argv = []`.
8. `TypeError: Object has no member 'binding'` — `process.binding("constants")` needed. Fixed: stub returning POSIX file open flag constants.
9. Promise not resolving (event loop deadlock) — `WebAssembly.instantiate` goroutine needed to call `loop.RunOnLoop` to resolve the promise on the event loop thread. Switched from `loop.Run()` to `loop.Start()`/`loop.Stop()` with a done channel.
10. `instantiate: unexpected bytes type map[string]interface{}` — `readFile` returned `{ buffer: ArrayBuffer }`; instantiate received the whole object. Fixed: extract `.buffer` from map.
11. `TypeError: Object has no member 'subarray'` — **final blocker**. After `WebAssembly.instantiate` resolves, it returns stub exports with no real WASM functions. PGlite calls into the exports (e.g., to initialize Postgres), gets `undefined`, and then calls `.subarray` on it.

**Root cause of final blocker:** To bridge `WebAssembly.instantiate` properly, we need wazero to actually execute the WASM and return real function exports back to goja. But instantiating `pglite.wasm` in wazero requires solving the same Approach 1 problem — the `env` module needs both memory/table AND functions. There is no way around this with wazero.

**What would be needed to fully implement Approach 2:**
- A WASM runtime that accepts JS-provided imports (memory, functions) and returns exports as callable objects — i.e., a real `WebAssembly` implementation inside goja
- OR: hand-implement all env function stubs in wazero, accepting that the only call paths through them will be Emscripten's internal startup (not a real Postgres execution)
- In practice, this requires a complete Emscripten runtime emulation layer, which is essentially reimplementing a full browser/node JS engine

**Conclusion:** Approach 2 can run PGlite's JS wrapper but cannot actually execute the WASM. Not viable as a pure Go approach.

---

## Approach 3: Node.js subprocess (WORKING)

**What was tried:** Embed all PGlite assets (`pglite.wasm`, `pglite.data`, `pglite.cjs`, `index.cjs`, `initdb.wasm`) into the Go binary. On startup: extract to tmpdir, spawn a Node.js process running a bundled `.mjs` script, communicate results via JSON-lines on stdout.

**Working query result:**
```json
[{"id":1,"name":"gomanan","sha":"abc123","deployed_at":"2026-03-19T00:00:00Z"}]
```

**Limitations:**
- Requires Node.js on the host machine — not truly self-contained
- ~60ms startup overhead per query session (process spawn + WASM init)
- Each `runApproach3()` call spins up a fresh Postgres instance — no persistence between calls without a tmpdir that survives across calls
- Not in-process: cannot share memory, transactions, or Go types with PGlite

---

## Binary Size

| Component | Size |
|-----------|------|
| `pglite.wasm` | 8.3 MB |
| `pglite.data` | 5.0 MB |
| `index.cjs` | 573 KB |
| `pglite.cjs` | 431 KB |
| `initdb.wasm` | 164 KB |
| **Total binary** | **32 MB** |

If the binary size matters, the assets (especially `pglite.data` and `pglite.wasm`) could be compressed or loaded from disk at runtime.

---

## Viability Assessment

### Can PGlite be embedded in a Go binary?

**As a subprocess (Approach 3): Yes, with caveats.**
- Works today, proven in this experiment
- Requires Node.js on the host — not suitable for shipping a self-contained binary
- Acceptable for a developer tool like Anito where Node.js is available
- Not suitable for production binaries that need zero external dependencies

**In-process (Approaches 1 & 2): No, not with current Go tooling.**

The fundamental blocker is Emscripten's side-module format. PGlite's WASM binary is not a standalone module — it is a dynamically-linked library that requires a full Emscripten runtime (dynamic linker, POSIX filesystem, threading primitives). This runtime is provided by the JavaScript glue code in `index.cjs`.

Pure Go WASM runtimes (wazero) and pure Go JS engines (goja) cannot cooperate to provide this runtime because:
1. The Emscripten JS glue owns memory, table, and globals for the WASM module
2. The WASM module needs the JS engine's native `WebAssembly` implementation to run
3. goja does not have a native `WebAssembly` implementation — it would need to be built from scratch
4. wazero cannot accept externally-created memory objects

### What would make in-process embedding viable?

1. **Use a CGO-based WASM runtime** (e.g., `wasmtime-go` wrapping Wasmtime's C API) which supports the Emscripten dynamic linking ABI. This could provide all env imports correctly.

2. **Use a WASM-to-native compiler** like `wasm2c` or TinyGo's ahead-of-time WASM compiler to compile `pglite.wasm` to native Go/C code, bypassing the runtime problem.

3. **Use a different PGlite build** — if Electric SQL offers a build that doesn't use Emscripten dynamic linking (a standalone WASM module), it would work with wazero directly.

4. **Use libpq/PostgreSQL directly** — skip PGlite entirely and use `cgo` to embed libpq, or use a pure-Go Postgres reimplementation like `pgvector-go` or `zombiezen.com/go/sqlite` (for SQLite instead of Postgres).

### Recommendation for Anito

For Anito's registry (the use case that motivated this experiment): **use `zombiezen.com/go/sqlite` or `modernc.org/sqlite`** (pure Go, no CGO). SQLite is sufficient for storing service registrations, deploy history, and port assignments. It embeds cleanly into a single Go binary with zero external dependencies. PGlite is a browser/Node.js tool — it is not designed for Go embedding.

If Postgres SQL syntax compatibility is required, consider `zombiezen.com/go/sqlite` with the JSON1 extension, or `rqlite/sql` which provides SQLite with a Postgres-compatible query layer.
