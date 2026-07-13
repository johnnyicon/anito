package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	portRangeMin = 8100
	portRangeMax = 8200

	// ManagedBlockStart / End delimit Anito-generated blocks in source files.
	// The setup tool writes these markers; anito_coordinate --refresh can locate
	// and update them without touching surrounding code.
	ManagedBlockStart = "// [anito:managed start]"
	ManagedBlockEnd   = "// [anito:managed end]"
)

// ServiceSpec describes one service in a composite app.
type ServiceSpec struct {
	Name          string
	Path          string   // absolute path to the service root directory
	PreferredPort int      // 0 = auto-allocate from portRangeMin–portRangeMax
	Language      Language // auto-detected if empty
	Framework     string   // "vite", "next", "go", "node" — auto-detected if empty
}

// Relationship describes a runtime dependency between two services.
// From knows about To's stable address via an env var injected by Anito.
type Relationship struct {
	From      string // service name that needs the address
	To        string // service name being depended on
	ProxyPath string // optional: HTTP path to forward in Vite dev server (e.g. "/api")
}

// PortAllocation maps service name → assigned stable port.
type PortAllocation map[string]int

// GeneratedFile is a file Anito instructs the LLM to write into the repo.
type GeneratedFile struct {
	RelPath string // path relative to the repo root
	Content string
}

// SourcePatch describes an [anito:managed] block to apply to an existing source file.
// The LLM finds or creates the managed block (by marker) and replaces its content.
type SourcePatch struct {
	RelPath     string // path relative to the repo root
	Marker      string // human-readable marker name (used in comments and block delimiters)
	Block       string // the complete managed block, including start/end comments
	Instruction string // plain-language instruction for the LLM applying this patch
}

// CoordinationResult is the output of CoordinateApp.
type CoordinationResult struct {
	Allocations    PortAllocation
	PortsEnvPath   string // always ".anito/ports.env"
	GeneratedFiles []GeneratedFile
	SourcePatches  []SourcePatch
	Instructions   []string
}

// CoordinateApp is the engine behind the anito_coordinate MCP tool.
// It assigns stable ports to all services, generates the shared ports.env,
// per-service config.yaml files, and [anito:managed] source patches for
// frameworks that need them (e.g. Vite proxy config).
//
// usedPorts is the set of already-claimed ports from the Anito registry
// (call registry.UsedPorts() and pass it here).
func CoordinateApp(
	repoPath string,
	services []ServiceSpec,
	relationships []Relationship,
	usedPorts map[int]bool,
) (*CoordinationResult, error) {
	// Auto-detect language + framework for services that don't specify them.
	detected := make([]ServiceSpec, len(services))
	copy(detected, services)
	for i := range detected {
		if detected[i].Language == "" {
			detected[i].Language = detectLanguage(detected[i].Path)
		}
		if detected[i].Framework == "" {
			detected[i].Framework = detectFramework(detected[i].Path, detected[i].Language)
		}
	}

	alloc, err := AllocatePorts(detected, usedPorts)
	if err != nil {
		return nil, err
	}

	result := &CoordinationResult{
		Allocations:  alloc,
		PortsEnvPath: ".anito/ports.env",
	}

	// Shared ports.env
	result.GeneratedFiles = append(result.GeneratedFiles, GeneratedFile{
		RelPath: ".anito/ports.env",
		Content: generatePortsEnv(detected, alloc),
	})

	// Per-service config.yaml + dev wrapper scripts where needed
	for _, svc := range detected {
		cfgFile, scriptFile := generateServiceFiles(svc, alloc[svc.Name], repoPath)
		result.GeneratedFiles = append(result.GeneratedFiles, cfgFile)
		if scriptFile != nil {
			result.GeneratedFiles = append(result.GeneratedFiles, *scriptFile)
		}
	}

	// Source patches for frameworks that need them
	for _, svc := range detected {
		if svc.Framework == "vite" || svc.Framework == "next" {
			if patch := generateNodeFrameworkPatch(svc, alloc, relationships, repoPath); patch != nil {
				result.SourcePatches = append(result.SourcePatches, *patch)
			}
		}
	}

	result.Instructions = buildInstructions(repoPath, detected, alloc, result)
	return result, nil
}

// AllocatePorts assigns stable ports to services without touching the registry.
// usedPorts includes currently registered ports (from registry.UsedPorts()).
// Anito's own reserved ports (7700, 7701) are always excluded.
func AllocatePorts(services []ServiceSpec, usedPorts map[int]bool) (PortAllocation, error) {
	taken := map[int]bool{7700: true, 7701: true}
	for p := range usedPorts {
		taken[p] = true
	}
	alloc := make(PortAllocation, len(services))

	// First pass: honour preferred ports.
	for _, svc := range services {
		if svc.PreferredPort != 0 && !taken[svc.PreferredPort] {
			alloc[svc.Name] = svc.PreferredPort
			taken[svc.PreferredPort] = true
		}
	}

	// Second pass: auto-allocate the rest from the range.
	next := portRangeMin
	for _, svc := range services {
		if alloc[svc.Name] != 0 {
			continue
		}
		for taken[next] {
			next++
		}
		if next > portRangeMax {
			return nil, fmt.Errorf("no available ports in range %d–%d", portRangeMin, portRangeMax)
		}
		alloc[svc.Name] = next
		taken[next] = true
		next++
	}
	return alloc, nil
}

// detectFramework returns the primary framework for a Node service.
func detectFramework(path string, lang Language) string {
	if lang == Go {
		return "go"
	}
	if lang != Node {
		return string(lang)
	}
	for _, f := range []string{"vite.config.ts", "vite.config.js", "vite.config.mts"} {
		if _, err := os.Stat(filepath.Join(path, f)); err == nil {
			return "vite"
		}
	}
	for _, f := range []string{"next.config.js", "next.config.ts", "next.config.mjs"} {
		if _, err := os.Stat(filepath.Join(path, f)); err == nil {
			return "next"
		}
	}
	return "node"
}

// envPrefix converts a service name to its ENV_VAR prefix.
// "tahua-www" → "TAHUA_WWW"
func envPrefix(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// generatePortsEnv builds the content of .anito/ports.env.
func generatePortsEnv(services []ServiceSpec, alloc PortAllocation) string {
	var sb strings.Builder
	sb.WriteString("# [anito:managed] DO NOT EDIT — regenerate with: anito setup\n")
	sb.WriteString("# Stable port assignments for this composite app, managed by Anito.\n")
	sb.WriteString("# Each service's config.yaml references this file via env_file.\n")
	sb.WriteString("# Source files read these env vars to know their neighbours' addresses.\n\n")
	for _, svc := range services {
		port := alloc[svc.Name]
		prefix := envPrefix(svc.Name)
		sb.WriteString(fmt.Sprintf("# %s — address never changes on redeploy\n", svc.Name))
		sb.WriteString(fmt.Sprintf("%s_PORT=%d\n", prefix, port))
		sb.WriteString(fmt.Sprintf("%s_URL=http://localhost:%d\n\n", prefix, port))
	}
	return sb.String()
}

// generateServiceFiles returns the config.yaml GeneratedFile for a service and,
// for Vite dev services, an optional wrapper shell script.
func generateServiceFiles(svc ServiceSpec, port int, repoRoot string) (GeneratedFile, *GeneratedFile) {
	configPath := fmt.Sprintf(".anito/%s.yaml", svc.Name)
	var cfg strings.Builder
	cfg.WriteString(fmt.Sprintf("# .anito/%s.yaml\n", svc.Name))
	cfg.WriteString("# [anito:managed] DO NOT EDIT — regenerate with: anito setup\n")
	cfg.WriteString(fmt.Sprintf("name: %s\n", svc.Name))
	cfg.WriteString(fmt.Sprintf("port: %d\n", port))
	// ports.env lives alongside the config in .anito/ — use a path relative to
	// the config file so the CLI can resolve it consistently regardless of CWD.
	cfg.WriteString("env_file: ports.env\n")

	switch svc.Framework {
	case "vite":
		scriptRelPath := fmt.Sprintf(".anito/%s-dev.sh", svc.Name)
		cfg.WriteString("type: binary\n")
		cfg.WriteString(fmt.Sprintf("output: %s\n", scriptRelPath))
		cfg.WriteString("health_check: /\n")

		// Determine pnpm workspace filter if service is not at repo root
		rel, _ := filepath.Rel(repoRoot, svc.Path)
		var runCmd string
		if rel == "." || rel == "" {
			runCmd = "pnpm exec vite dev --force"
		} else {
			// Try to read package.json name for workspace filter
			pkgName := readPackageJSONName(svc.Path)
			if pkgName != "" {
				runCmd = fmt.Sprintf("pnpm --filter %s exec vite dev --force", pkgName)
			} else {
				runCmd = fmt.Sprintf("pnpm --filter ./%s exec vite dev --force", rel)
			}
		}

		var script strings.Builder
		script.WriteString("#!/usr/bin/env bash\n")
		script.WriteString("# [anito:managed] DO NOT EDIT — regenerate with: anito setup\n")
		script.WriteString(fmt.Sprintf("# Dev server wrapper for %s. Anito injects PORT via env.\n", svc.Name))
		script.WriteString("# Vite --force clears stale module/dependency cache after worktree branch changes.\n")
		script.WriteString(fmt.Sprintf("cd %s\n", repoRoot))
		script.WriteString(fmt.Sprintf("exec %s\n", runCmd))

		scriptFile := &GeneratedFile{
			RelPath: scriptRelPath,
			Content: script.String(),
		}
		return GeneratedFile{RelPath: configPath, Content: cfg.String()}, scriptFile

	case "go":
		buildCmd, outputPath := inferBuildAndOutput(svc.Path, svc.Name, Go)
		if buildCmd != "" {
			cfg.WriteString(fmt.Sprintf("build: %s\n", buildCmd))
		}
		if outputPath != "" {
			cfg.WriteString(fmt.Sprintf("output: %s\n", outputPath))
		}
		cfg.WriteString("type: binary\n")
		cfg.WriteString("health_check: /health\n")

	default:
		cfg.WriteString("type: binary\n")
		cfg.WriteString(fmt.Sprintf("output: ./%s\n", svc.Name))
		cfg.WriteString("health_check: /health\n")
	}

	return GeneratedFile{RelPath: configPath, Content: cfg.String()}, nil
}

// generateNodeFrameworkPatch generates the [anito:managed] source patch for
// Vite and Next.js services — the server port + proxy config that must be
// wired to Anito's env vars.
func generateNodeFrameworkPatch(svc ServiceSpec, alloc PortAllocation, relationships []Relationship, repoRoot string) *SourcePatch {
	prefix := envPrefix(svc.Name)
	port := alloc[svc.Name]

	// Resolve the config file path relative to repo root.
	var configFileName, configFilePath string
	candidates := []string{"vite.config.ts", "vite.config.js", "next.config.ts", "next.config.js", "next.config.mjs"}
	for _, f := range candidates {
		if _, err := os.Stat(filepath.Join(svc.Path, f)); err == nil {
			configFileName = f
			rel, _ := filepath.Rel(repoRoot, filepath.Join(svc.Path, f))
			configFilePath = rel
			break
		}
	}
	if configFileName == "" {
		// Fall back: assume vite.config.ts in service root
		configFileName = "vite.config.ts"
		rel, _ := filepath.Rel(repoRoot, filepath.Join(svc.Path, configFileName))
		configFilePath = rel
	}

	// Collect proxy entries: services that this service depends on.
	type proxyEntry struct {
		path     string
		toName   string
		toPrefix string
		toPort   int
	}
	var proxies []proxyEntry
	for _, rel := range relationships {
		if rel.From == svc.Name {
			toPort := alloc[rel.To]
			proxyPath := rel.ProxyPath
			if proxyPath == "" {
				proxyPath = "/api"
			}
			proxies = append(proxies, proxyEntry{
				path:     proxyPath,
				toName:   rel.To,
				toPrefix: envPrefix(rel.To),
				toPort:   toPort,
			})
		}
	}

	var block strings.Builder
	block.WriteString("  // [anito:managed start] — DO NOT EDIT. Regenerate with: anito setup\n")
	block.WriteString("  // Port and proxy config assigned by Anito for composite app coordination.\n")
	block.WriteString("  // These env vars are injected by Anito from .anito/ports.env at startup.\n")
	block.WriteString("  // Changing them manually will break service coordination.\n")
	block.WriteString("  server: {\n")
	block.WriteString(fmt.Sprintf("    port: parseInt(process.env.%s_PORT ?? '') || %d,\n", prefix, port))
	if len(proxies) > 0 {
		block.WriteString("    proxy: {\n")
		for _, p := range proxies {
			block.WriteString(fmt.Sprintf("      // → %s (stable address: http://localhost:%d)\n", p.toName, p.toPort))
			block.WriteString(fmt.Sprintf("      '%s': process.env.%s_URL ?? 'http://localhost:%d',\n", p.path, p.toPrefix, p.toPort))
		}
		block.WriteString("    },\n")
	}
	block.WriteString("  },\n")
	block.WriteString("  // [anito:managed end]\n")

	instruction := fmt.Sprintf(
		"In %s, replace (or add) the `server:` block inside `defineConfig({...})` with the block below.\n"+
			"The env vars are loaded from .anito/ports.env by Anito at startup — do not hardcode the values.\n\n%s",
		configFileName, block.String(),
	)

	return &SourcePatch{
		RelPath:     configFilePath,
		Marker:      "server",
		Block:       block.String(),
		Instruction: instruction,
	}
}

// buildInstructions generates the ordered action list for the LLM.
func buildInstructions(repoPath string, services []ServiceSpec, alloc PortAllocation, result *CoordinationResult) []string {
	var steps []string
	n := 1

	steps = append(steps, fmt.Sprintf(
		"%d. Write .anito/ports.env to the repo root. This is the shared address map — all services read it at startup.",
		n,
	))
	n++

	for _, svc := range services {
		steps = append(steps, fmt.Sprintf(
			"%d. Write .anito/%s.yaml. Port %d is now reserved for this service.",
			n, svc.Name, alloc[svc.Name],
		))
		n++
	}

	// Wrapper scripts
	for _, f := range result.GeneratedFiles {
		if strings.HasSuffix(f.RelPath, "-dev.sh") {
			steps = append(steps, fmt.Sprintf(
				"%d. Write %s and make it executable: chmod +x %s",
				n, f.RelPath, f.RelPath,
			))
			n++
		}
	}

	for _, patch := range result.SourcePatches {
		steps = append(steps, fmt.Sprintf(
			"%d. Apply source patch to %s:\n   %s",
			n, patch.RelPath, patch.Instruction,
		))
		n++
	}

	steps = append(steps, fmt.Sprintf(
		"%d. Reserve ports in Anito (call anito_reserve for each service, or deploy with the generated config). "+
			"Ports are reserved atomically — no conflicts possible after this step.",
		n,
	))
	n++

	steps = append(steps, fmt.Sprintf(
		"%d. Deploy all services. The ports below are permanent:\n",
		n,
	))
	for _, svc := range services {
		steps = append(steps,
			fmt.Sprintf("   anito deploy .anito/%s.yaml  →  http://localhost:%d", svc.Name, alloc[svc.Name]),
		)
	}

	return steps
}

// readPackageJSONName reads the "name" field from a package.json file.
// Returns empty string on any error.
func readPackageJSONName(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	// Simple extraction without full JSON parse to avoid import cycle.
	content := string(data)
	const key = `"name":`
	idx := strings.Index(content, key)
	if idx == -1 {
		return ""
	}
	rest := strings.TrimSpace(content[idx+len(key):])
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	end := strings.Index(rest[1:], `"`)
	if end == -1 {
		return ""
	}
	return rest[1 : end+1]
}
