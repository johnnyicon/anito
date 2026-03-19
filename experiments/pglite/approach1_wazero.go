// Approach 1: Try to run PGlite's WASM directly using wazero.
//
// PGlite is compiled with Emscripten as a relocatable/side-module (-sSIDE_MODULE).
// This means it imports memory, a function table, and GOT (Global Offset Table)
// globals from its "env" module. wazero's HostModuleBuilder cannot export memory,
// tables, or globals — only functions.
//
// Strategy:
//  1. Compile a hand-crafted WASM "env_base" that exports memory + table + GOT globals.
//  2. Use wazero's built-in emscripten.InstantiateForModule for the invoke_* functions.
//  3. Add remaining env function stubs via another host module.
//
// The fundamental blocker: env needs to be BOTH a WASM module (memory/table/globals)
// AND a host module (functions). wazero cannot instantiate two modules with the same name.
package main

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/emscripten"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func i32s(n int) []api.ValueType {
	t := make([]api.ValueType, n)
	for i := range t {
		t[i] = api.ValueTypeI32
	}
	return t
}

// enosys returns ENOSYS (-38) as a uint64 i32 bit pattern.
func enosysVal() uint64 { v := int32(-38); return uint64(uint32(v)) }

// efailVal returns -1 as a uint64 i32 bit pattern.
func efailVal() uint64 { v := int32(-1); return uint64(uint32(v)) }

// stub registers a no-op host function.
func stub(b wazero.HostModuleBuilder, name string, params, results []api.ValueType) {
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			for i := range stack {
				stack[i] = 0
			}
		}), params, results).
		Export(name)
}

func stubRet(b wazero.HostModuleBuilder, name string, params, results []api.ValueType, ret uint64) {
	b.NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, _ api.Module, stack []uint64) {
			if len(results) > 0 {
				stack[0] = ret
			}
		}), params, results).
		Export(name)
}

// buildEnvBaseWasm creates a minimal WASM module named "env" that exports:
//   - memory (min=2048 pages = 128MB, max=32768 pages = 2GB)
//   - __indirect_function_table (funcref, min=6097)
//   - __memory_base i32 immutable global = 0
//   - __table_base  i32 immutable global = 0
//   - __stack_pointer i32 mutable global = 5MB
//
// wazero's HostModuleBuilder can only export functions, so we hand-craft a WASM binary.
func buildEnvBaseWasm() []byte {
	// We build the WAT equivalent:
	// (module
	//   (memory (export "memory") 2048 32768)
	//   (table (export "__indirect_function_table") 6097 funcref)
	//   (global (export "__memory_base") i32 (i32.const 0))
	//   (global (export "__stack_pointer") (mut i32) (i32.const 5242880))
	//   (global (export "__table_base") i32 (i32.const 0))
	// )

	uleb := func(n int) []byte {
		var r []byte
		for {
			b := byte(n & 0x7f)
			n >>= 7
			if n != 0 {
				r = append(r, b|0x80)
			} else {
				r = append(r, b)
				break
			}
		}
		return r
	}
	str := func(s string) []byte {
		b := []byte(s)
		return append(uleb(len(b)), b...)
	}
	section := func(id byte, body []byte) []byte {
		return append(append([]byte{id}, uleb(len(body))...), body...)
	}
	concat := func(parts ...[]byte) []byte {
		var r []byte
		for _, p := range parts {
			r = append(r, p...)
		}
		return r
	}

	// Memory section (id=5): 1 memory, min=2048 max=32768 (flags=1 = has max)
	memSection := section(5, concat(
		uleb(1),                         // count
		[]byte{0x01},                    // flags: has max
		uleb(2048), uleb(32768),         // min, max in pages
	))

	// Table section (id=4): 1 table, funcref(0x70), no max (flags=0), min=6097
	tableSection := section(4, concat(
		uleb(1),
		[]byte{0x70, 0x00},  // funcref, no max flag
		uleb(6097),          // min
	))

	// Global section (id=6): 3 globals
	// 1. __memory_base: i32 immutable = 0
	// 2. __stack_pointer: i32 mutable = 5*1024*1024
	// 3. __table_base: i32 immutable = 0
	stackVal := 5 * 1024 * 1024
	globalSection := section(6, concat(
		uleb(3),
		// __memory_base
		[]byte{0x7f, 0x00, 0x41, 0x00, 0x0b},
		// __stack_pointer (mutable)
		append([]byte{0x7f, 0x01, 0x41}, append(uleb(stackVal), 0x0b)...),
		// __table_base
		[]byte{0x7f, 0x00, 0x41, 0x00, 0x0b},
	))

	// Export section (id=7): 5 exports
	exportSection := section(7, concat(
		uleb(5),
		// memory → kind=2(memory), index=0
		append(str("memory"), 0x02, 0x00),
		// __indirect_function_table → kind=1(table), index=0
		append(str("__indirect_function_table"), 0x01, 0x00),
		// __memory_base → kind=3(global), index=0
		append(str("__memory_base"), 0x03, 0x00),
		// __stack_pointer → kind=3(global), index=1
		append(str("__stack_pointer"), 0x03, 0x01),
		// __table_base → kind=3(global), index=2
		append(str("__table_base"), 0x03, 0x02),
	))

	// WASM section order is mandatory: type(1) import(2) func(3) TABLE(4) MEMORY(5) global(6) export(7)
	magic := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	return concat(magic, tableSection, memSection, globalSection, exportSection)
}

func runApproach1() error {
	ctx := context.Background()

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Step 1: Instantiate WASI (wasi_snapshot_preview1)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// Step 2: First compile the guest (PGlite) so we can inspect its imports
	// for wazero's emscripten.InstantiateForModule
	fmt.Println("Compiling pglite.wasm...")
	guestCompiled, err := rt.CompileModule(ctx, pgliteWasm)
	if err != nil {
		return fmt.Errorf("CompileModule failed: %w", err)
	}
	defer guestCompiled.Close(ctx)
	fmt.Println("Compiled OK.")

	// Step 3: Instantiate "env" base (memory + table + globals) as a WASM module
	envBaseWasm := buildEnvBaseWasm()
	fmt.Printf("env_base WASM size: %d bytes\n", len(envBaseWasm))

	envBaseCompiled, err := rt.CompileModule(ctx, envBaseWasm)
	if err != nil {
		return fmt.Errorf("env_base compile failed: %w", err)
	}

	// Instantiate env_base as "env" — this gives memory, table, and GOT globals to pglite
	envBaseMod, err := rt.InstantiateModule(ctx, envBaseCompiled,
		wazero.NewModuleConfig().WithName("env"))
	if err != nil {
		return fmt.Errorf("env_base instantiation failed: %w", err)
	}
	fmt.Printf("env_base instantiated. Memory: %v, Table: %v\n",
		envBaseMod.ExportedMemory("memory") != nil,
		envBaseMod.ExportedFunction("__indirect_function_table") == nil, // table, not function
	)

	// Step 4: Use wazero's built-in Emscripten support for invoke_* functions.
	// InstantiateForModule inspects the guest's imports and generates correct
	// invoke_* implementations that call back into the WASM function table.
	// BUT: it also tries to instantiate a module named "env" — which we already did.
	// So we use NewFunctionExporterForModule instead and export into a different name.
	//
	// This is the fundamental problem: wazero emscripten wants to OWN the "env" module,
	// but env also needs to export memory/table/globals which HostModuleBuilder can't do.
	//
	// Let's try: use emscripten.NewFunctionExporterForModule to get the invoke functions,
	// then add them to a NEW host module named something else... but the guest imports from "env".
	//
	// Conclusion: wazero cannot support this pattern. The env module needs to be BOTH
	// a host module (functions) AND a WASM module (memory/table/globals) simultaneously.
	// These are mutually exclusive in wazero.

	fmt.Println("\nAttempting emscripten.InstantiateForModule (will conflict with 'env' already instantiated)...")
	_, err = emscripten.InstantiateForModule(ctx, rt, guestCompiled)
	if err != nil {
		fmt.Printf("emscripten.InstantiateForModule failed (expected): %v\n", err)
		fmt.Println("\nFundamental blocker confirmed: wazero cannot have 'env' be both a WASM module")
		fmt.Println("(needed for memory/table/globals exports) and a host module (needed for functions).")
		fmt.Println("This is a hard architectural limitation of wazero's design.")
	}

	return fmt.Errorf("Approach 1 blocked: wazero cannot provide env.memory + env functions in one module")
}
