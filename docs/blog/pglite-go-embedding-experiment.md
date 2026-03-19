# Can You Embed PGlite in a Go Binary? We Tried.

*March 19, 2026*

---

We're building Anito — a local production service manager for macOS. It's a single Go binary: daemon, CLI, and MCP server all in one. No sidecar processes, no Docker, no infrastructure overhead. Just `anito deploy` and your service is running at a stable localhost port forever.

We wanted to add a deployment log — a record of every deploy: timestamp, service name, git SHA, stable port. Simple stuff. But we also wanted to be able to query it. "Show me all deploys for gomanan in the last 7 days." That kind of thing.

The obvious Go answer is SQLite. But we like Postgres. We write Postgres day-to-day. We know its query language, its `TIMESTAMPTZ`, its `SERIAL`. When someone mentioned PGlite — a full PostgreSQL engine compiled to WebAssembly, running in-process with no server — we got excited. Could we embed it in our Go binary the same way we embed a React SPA?

We decided to find out.

---

## What is PGlite?

[PGlite](https://pglite.dev) is a project by Electric SQL. It's PostgreSQL — the real thing, not a reimplementation — compiled to WebAssembly via Emscripten. It runs in the browser and in Node.js. You get full SQL: transactions, JSON operators, `TIMESTAMPTZ`, extensions. The dream for us was: embed PGlite's WASM assets in the Go binary using `//go:embed`, run it in-process, and get a zero-dependency local Postgres database.

We already embed a 2MB React SPA this way. A WASM binary felt like the same idea.

---

## Three approaches, one afternoon

We ran the experiment in an isolated Git worktree to keep main clean. Three approaches, tried in order.

### Approach 1: wazero + raw WASM

[wazero](https://wazero.io) is a pure-Go WebAssembly runtime — no CGO, zero native dependencies. The plan: instantiate `pglite.wasm` directly in wazero, provide the host imports it needs, and talk to it from Go.

The WASM file is 8.3MB. We pulled it from the `@electric-sql/pglite` npm package. We identified the 117 host function imports PGlite needs from the `"env"` module and stubbed them out in wazero.

It failed immediately with:

```
module[env] has already been instantiated
```

The root cause is architectural. PGlite is compiled with Emscripten's **side-module** format (`-sSIDE_MODULE`). This means `pglite.wasm` imports from a single module named `"env"`, and that module must simultaneously export:

- **Memory** (`env.memory`) — a WASM linear memory object
- **A function table** (`env.__indirect_function_table`) — a WASM table
- **GOT globals** (`env.__memory_base`, `env.__stack_pointer`, etc.)
- **117 host functions** (`invoke_*`, POSIX syscalls, Emscripten runtime functions)

Here's the problem: wazero cannot create a single module that exports both memory/table/globals (which require a WASM module definition) AND host functions (which require a host function module). You can't instantiate two modules with the same name `"env"`. This is a hard architectural constraint in wazero — not a missing feature, not a workaround waiting to be found.

**Verdict: blocked.**

---

### Approach 2: goja (pure Go JS engine) + PGlite's JS bundle

PGlite ships as a JavaScript module (`index.cjs`, 573KB) that wraps the WASM with the Emscripten runtime. The JS glue owns memory, handles the dynamic linker, provides POSIX filesystem shims — all the things wazero can't do.

[goja](https://github.com/dop251/goja) is a pure-Go JavaScript engine. The plan: run PGlite's JS bundle inside goja, bridge `WebAssembly.instantiate` back to wazero for compilation, and let the JS glue handle everything Emscripten needs.

This one got *surprisingly far*. We fixed 11 sequential errors:

1. goja doesn't support dynamic `import()` — patched `index.cjs` to use a sync require stub
2. Source map reference causing a load error — stripped
3. Wrong bundle (low-level glue vs full PGlite class) — switched files
4. Missing `URL` constructor — shimmed
5. Missing `TextEncoder.prototype.encodeInto` — shimmed
6. `require("module").createRequire()` not threading through correctly — fixed
7. `process.argv` undefined — added stub
8. `process.binding("constants")` needed POSIX file open flags — stubbed
9. Event loop deadlock in Promise resolution — switched from `loop.Run()` to `loop.Start()`/`loop.Stop()` with a done channel
10. `WebAssembly.instantiate` receiving the wrong type — extracted `.buffer` from a map
11. **Final blocker:** `TypeError: Object has no member 'subarray'` — undefined where a real function export was expected

We got PGlite to load. We got it to compile the WASM. We got it to resolve the Promise. But when PGlite called into the WASM exports to initialize Postgres, it got stubs — because actually executing `pglite.wasm` inside wazero has the same Approach 1 problem. The `env` module still needs both memory and functions simultaneously.

To fully implement Approach 2, you'd need a complete in-process `WebAssembly` implementation inside goja — one that accepts JS-provided imports (memory, functions) and returns real callable exports. That's essentially building a browser runtime from scratch.

**Verdict: blocked at the same root cause as Approach 1.**

---

### Approach 3: Node.js subprocess (working, but...)

We embedded all PGlite assets in the Go binary using `//go:embed`, extracted them to a tmpdir at runtime, and spawned a Node.js process running a bundled `.mjs` script. The Go process communicates with Node via JSON-lines on stdout.

This works. The output:

```json
[{"id":1,"name":"gomanan","sha":"abc123","deployed_at":"2026-03-19T00:00:00Z"}]
```

But the limitations are obvious:
- Requires Node.js on the host — not self-contained
- ~60ms startup per query session
- Not in-process: no shared memory, no Go transactions
- 32MB binary overhead (8.3MB WASM + 5MB data files + JS bundles)

For Anito — a tool for developers who definitely have Node.js installed — this is *technically usable*. But it violates the single-binary principle we care about. The whole point of Anito is zero infrastructure overhead.

**Verdict: works, not suitable.**

---

## Why it can't be done (the real explanation)

The fundamental blocker is Emscripten's dynamic linking ABI. `pglite.wasm` is not a standalone WASM module. It's a dynamically-linked side module that depends on a full Emscripten runtime environment: a dynamic linker, a POSIX filesystem simulation, threading primitives, and a memory model where the JS host owns the linear memory.

This runtime is implemented in JavaScript — it's the glue code in `index.cjs`. There is no pure-Go implementation of it. To run PGlite in-process in Go, you'd need:

1. A WASM runtime that accepts externally-created memory objects (wazero doesn't)
2. A JS engine with a native `WebAssembly` implementation (goja doesn't have one)
3. Both working together, sharing the same memory model

None of the current pure-Go tooling supports this combination. The only viable path would be a CGO-based WASM runtime (like [wasmtime-go](https://github.com/bytecodealliance/wasmtime-go)) that supports Emscripten's dynamic linking ABI — but that breaks the "no CGO" property we need for a truly self-contained binary.

---

## What we're doing instead

For the deployment log, we're going with `modernc.org/sqlite` — a pure-Go port of SQLite (transpiled from C to Go, no CGO required). It embeds cleanly in a single binary, has no external dependencies, and for a table like:

```sql
CREATE TABLE deployments (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL,
  sha         TEXT NOT NULL,
  stable_port INTEGER NOT NULL,
  deployed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

...the SQL is identical to what we'd write in Postgres.

The experiment was worth running. We learned something real about Emscripten's dynamic linking model and the gap between "WASM in the browser" and "WASM in a Go binary." And the worktree is preserved on the `experiment/pglite-embedding` branch — if the tooling ever catches up, we'll know exactly where to pick it up.

---

## The experiment code

The full experiment lives in `experiments/pglite/` on the [`experiment/pglite-embedding`](https://github.com/johnnyicon/anito/tree/experiment/pglite-embedding/experiments/pglite) branch. It includes working implementations of all three approaches, a detailed REPORT.md with exact error messages at each failure point, and the embedded PGlite assets. If you want to dig further — particularly with a CGO-based WASM runtime — start there.
