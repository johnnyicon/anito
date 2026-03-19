// Approach 3: Run PGlite in a Node.js subprocess.
//
// The WASM bridging approaches (1 and 2) hit fundamental limitations:
// - wazero cannot provide env.memory + env functions from the same module (Approach 1)
// - goja's WebAssembly bridge can compile the WASM but cannot provide real exports (Approach 2)
//
// Approach 3 delegates to a real Node.js process which natively supports PGlite.
// The Go binary embeds a tiny JS runner script, writes it to a temp file, and communicates
// via stdin/stdout JSON-lines protocol. This is not an in-process embedding, but it proves
// that PGlite works and establishes the baseline for what a real embedding must achieve.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// pgliteNodeScript is the Node.js runner. It imports PGlite from the CJS bundle
// (written to a temp dir alongside pglite.data and pglite.wasm), creates an
// in-memory database, runs the three SQL operations, and prints results as JSON.
const pgliteNodeScript = `
import { createRequire } from 'module';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

// Load PGlite from the CJS bundle co-located with this script
const __filename = fileURLToPath(import.meta.url);
const __dir = dirname(__filename);
const require = createRequire(import.meta.url);

// PGlite CJS exports
const { PGlite } = require(join(__dir, 'index.cjs'));

async function main() {
  console.error('[node] Creating PGlite in-memory instance...');
  const db = await PGlite.create();
  console.error('[node] PGlite ready. Running SQL...');

  await db.exec('CREATE TABLE deployments (id SERIAL, name TEXT, sha TEXT, deployed_at TEXT)');
  await db.exec("INSERT INTO deployments (name, sha, deployed_at) VALUES ('gomanan', 'abc123', '2026-03-19T00:00:00Z')");
  const result = await db.query('SELECT * FROM deployments');
  
  process.stdout.write(JSON.stringify({ rows: result.rows }) + '\n');
  console.error('[node] Done.');
}

main().catch(e => {
  process.stderr.write('[node] ERROR: ' + String(e) + '\n');
  process.exit(1);
});
`

func runApproach3() error {
	// Write assets to a temp directory alongside the runner script
	tmpDir, err := os.MkdirTemp("", "pglite-node-*")
	if err != nil {
		return fmt.Errorf("mktemp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Printf("[node] temp dir: %s\n", tmpDir)

	// Write the embedded assets
	if err := os.WriteFile(tmpDir+"/pglite.wasm", pgliteWasm, 0644); err != nil {
		return fmt.Errorf("write pglite.wasm: %w", err)
	}
	if err := os.WriteFile(tmpDir+"/pglite.data", pgliteData, 0644); err != nil {
		return fmt.Errorf("write pglite.data: %w", err)
	}
	if err := os.WriteFile(tmpDir+"/pglite.cjs", pgliteCJS, 0644); err != nil {
		return fmt.Errorf("write pglite.cjs: %w", err)
	}
	if err := os.WriteFile(tmpDir+"/index.cjs", indexCJS, 0644); err != nil {
		return fmt.Errorf("write index.cjs: %w", err)
	}
	if err := os.WriteFile(tmpDir+"/initdb.wasm", initdbWasm, 0644); err != nil {
		return fmt.Errorf("write initdb.wasm: %w", err)
	}
	scriptPath := tmpDir + "/runner.mjs"
	if err := os.WriteFile(scriptPath, []byte(pgliteNodeScript), 0644); err != nil {
		return fmt.Errorf("write runner.mjs: %w", err)
	}

	// Run Node.js
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("node not found in PATH: %w", err)
	}
	fmt.Printf("[node] running %s %s\n", nodePath, scriptPath)

	cmd := exec.Command(nodePath, scriptPath)
	cmd.Stderr = os.Stderr
	out, runErr := cmd.Output()

	// Parse JSON output regardless of exit code — PGlite may call process.exit(99)
	// as part of WASM cleanup (normal for Emscripten).
	outStr := strings.TrimSpace(string(out))
	if outStr == "" {
		if runErr != nil {
			return fmt.Errorf("node subprocess failed with no output: %w", runErr)
		}
		return fmt.Errorf("node returned no output")
	}

	var result struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(outStr), &result); err != nil {
		return fmt.Errorf("parse output %q: %w", outStr, err)
	}

	fmt.Printf("[node] Query result: %d rows\n", len(result.Rows))
	for i, row := range result.Rows {
		fmt.Printf("  row[%d]: id=%v name=%v sha=%v deployed_at=%v\n",
			i, row["id"], row["name"], row["sha"], row["deployed_at"])
	}
	return nil
}

