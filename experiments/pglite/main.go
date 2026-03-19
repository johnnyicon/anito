// PGlite embedding experiment.
// Tries to embed PGlite (Electric SQL WASM PostgreSQL) into a Go binary.
//
// Approaches tried in order:
//  1. wazero + raw WASM  — fails: Emscripten side-module needs memory/table/globals
//     AND function imports all in one "env" module; wazero cannot do both.
//  2. goja (pure Go JS engine) — runs PGlite's CJS glue which internally calls
//     WebAssembly.instantiate; we bridge that back to wazero.
package main

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed pglite.wasm
var pgliteWasm []byte

//go:embed pglite.data
var pgliteData []byte

//go:embed pglite.cjs
var pgliteCJS []byte

//go:embed index.cjs
var indexCJS []byte

//go:embed initdb.wasm
var initdbWasm []byte

func main() {
	fmt.Printf("PGlite assets: wasm=%.1fMB data=%.1fMB cjs=%.0fKB\n",
		float64(len(pgliteWasm))/1024/1024,
		float64(len(pgliteData))/1024/1024,
		float64(len(pgliteCJS))/1024,
	)

	// Try approach 2 directly (approach 1's conclusion is already documented)
	fmt.Println("\n--- Approach 1: wazero + raw WASM ---")
	err1 := runApproach1()
	fmt.Printf("Approach 1 result: %v\n", err1)

	fmt.Println("\n--- Approach 2: goja + PGlite CJS ---")
	err2 := runApproach2()
	fmt.Printf("Approach 2 result: %v\n", err2)

	fmt.Println("\n--- Approach 3: Node.js subprocess ---")
	err3 := runApproach3()
	if err3 != nil {
		fmt.Fprintf(os.Stderr, "Approach 3 FAILED: %v\n", err3)
		os.Exit(1)
	}
	fmt.Println("Approach 3 succeeded!")
}
