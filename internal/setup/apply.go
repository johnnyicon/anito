package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PlanMode string

const (
	ModeSingle    PlanMode = "single"
	ModeComposite PlanMode = "composite"
)

type PlanRequest struct {
	Path          string
	Services      []ServiceSpec
	Relationships []Relationship
}

type PortCatalog interface {
	UsedPorts() map[int]bool
	StablePort(name string) (int, bool)
}

type PortReserver interface {
	Reserve(name string, preferredPort int) (int, error)
}

type ReservationRollbacker interface {
	Remove(name string) error
}

type Plan struct {
	Mode     PlanMode
	RepoPath string

	ServiceName    string
	Language       Language
	HasAnitoConfig bool
	HasPORT        bool
	HasHealthRoute bool
	Issues         []Issue

	Allocations PortAllocation

	GeneratedFiles []GeneratedFile
	SourcePatches  []SourcePatch
	Instructions   []string
}

type ApplyResult struct {
	Plan             *Plan
	Applied          bool
	AppliedFiles     []string
	AppliedPatches   []string
	UnappliedPatches []string
}

func DryRun(req PlanRequest, ports PortCatalog) (*Plan, error) {
	if len(req.Services) > 0 {
		return dryRunComposite(req, ports)
	}
	return dryRunSingle(req.Path)
}

func Apply(plan *Plan, reserver PortReserver) (*ApplyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("setup plan is required")
	}
	effective := plan.Clone()
	if effective.Mode == ModeSingle && effective.HasAnitoConfig {
		return nil, fmt.Errorf("%s already has .anito/config.yaml; call anito_deploy or anito_doctor instead of applying setup", effective.RepoPath)
	}

	if err := validateRepoRoot(effective.RepoPath); err != nil {
		return nil, err
	}
	if err := validatePlanPaths(effective); err != nil {
		return nil, err
	}

	var reservations []string
	rollbackReservations := func() {
		rollbacker, ok := reserver.(ReservationRollbacker)
		if !ok {
			return
		}
		for i := len(reservations) - 1; i >= 0; i-- {
			_ = rollbacker.Remove(reservations[i])
		}
	}

	if err := reservePlanPorts(effective, reserver, &reservations); err != nil {
		rollbackReservations()
		return nil, err
	}

	app, err := prepareApplication(effective)
	if err != nil {
		rollbackReservations()
		return nil, err
	}
	result := &ApplyResult{
		Plan:             effective,
		Applied:          true,
		UnappliedPatches: append([]string(nil), app.unapplied...),
	}
	if err := app.commit(result); err != nil {
		rollbackReservations()
		return nil, err
	}
	return result, nil
}

func (p *Plan) Clone() *Plan {
	if p == nil {
		return nil
	}
	out := *p
	out.Issues = append([]Issue(nil), p.Issues...)
	out.GeneratedFiles = append([]GeneratedFile(nil), p.GeneratedFiles...)
	out.SourcePatches = append([]SourcePatch(nil), p.SourcePatches...)
	out.Instructions = append([]string(nil), p.Instructions...)
	if p.Allocations != nil {
		out.Allocations = make(PortAllocation, len(p.Allocations))
		for name, port := range p.Allocations {
			out.Allocations[name] = port
		}
	}
	return &out
}

func dryRunSingle(path string) (*Plan, error) {
	result, err := Inspect(path)
	if err != nil {
		return nil, err
	}
	plan := &Plan{
		Mode:           ModeSingle,
		RepoPath:       result.RepoPath,
		ServiceName:    result.ServiceName,
		Language:       result.Language,
		HasAnitoConfig: result.HasAnitoConfig,
		HasPORT:        result.HasPORT,
		HasHealthRoute: result.HasHealthRoute,
		Issues:         append([]Issue(nil), result.Issues...),
		Instructions:   append([]string(nil), result.Instructions...),
	}
	if result.SuggestedConfig != "" {
		plan.GeneratedFiles = append(plan.GeneratedFiles, GeneratedFile{
			RelPath: ".anito/config.yaml",
			Content: result.SuggestedConfig,
		})
	}
	return plan, nil
}

func dryRunComposite(req PlanRequest, ports PortCatalog) (*Plan, error) {
	specs := make([]ServiceSpec, len(req.Services))
	copy(specs, req.Services)

	used := map[int]bool{}
	if ports != nil {
		for port, taken := range ports.UsedPorts() {
			used[port] = taken
		}
		for i := range specs {
			existing, ok := ports.StablePort(specs[i].Name)
			if !ok {
				continue
			}
			delete(used, existing)
			if specs[i].PreferredPort == 0 {
				specs[i].PreferredPort = existing
			}
		}
	}

	result, err := CoordinateApp(req.Path, specs, req.Relationships, used)
	if err != nil {
		return nil, err
	}
	return &Plan{
		Mode:           ModeComposite,
		RepoPath:       req.Path,
		Allocations:    copyAllocations(result.Allocations),
		GeneratedFiles: append([]GeneratedFile(nil), result.GeneratedFiles...),
		SourcePatches:  append([]SourcePatch(nil), result.SourcePatches...),
		Instructions:   append([]string(nil), result.Instructions...),
	}, nil
}

func reservePlanPorts(plan *Plan, reserver PortReserver, reservations *[]string) error {
	if plan.Mode == ModeSingle {
		if len(plan.Allocations) == 0 && reserver != nil && plan.ServiceName != "" {
			existed := stablePortExists(reserver, plan.ServiceName)
			port, err := reserver.Reserve(plan.ServiceName, 0)
			if err != nil {
				return fmt.Errorf("reserve %s: %w", plan.ServiceName, err)
			}
			if !existed {
				*reservations = append(*reservations, plan.ServiceName)
			}
			plan.Allocations = PortAllocation{plan.ServiceName: port}
			applySingleReservedPort(plan, port)
		}
		return nil
	}

	if len(plan.Allocations) == 0 {
		return nil
	}
	if reserver == nil {
		return fmt.Errorf("port reserver is required to apply composite setup")
	}
	names := make([]string, 0, len(plan.Allocations))
	for name := range plan.Allocations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		plannedPort := plan.Allocations[name]
		existed := stablePortExists(reserver, name)
		port, err := reserver.Reserve(name, plannedPort)
		if err != nil {
			return fmt.Errorf("reserve %s: %w", name, err)
		}
		if !existed {
			*reservations = append(*reservations, name)
		}
		if plannedPort != 0 && port != plannedPort {
			return fmt.Errorf("reserve %s: got port %d, wanted planned port %d", name, port, plannedPort)
		}
		plan.Allocations[name] = port
	}
	return nil
}

func stablePortExists(value any, name string) bool {
	catalog, ok := value.(interface {
		StablePort(name string) (int, bool)
	})
	if !ok {
		return false
	}
	_, exists := catalog.StablePort(name)
	return exists
}

func applySingleReservedPort(plan *Plan, port int) {
	for i := range plan.GeneratedFiles {
		if plan.GeneratedFiles[i].RelPath != ".anito/config.yaml" {
			continue
		}
		plan.GeneratedFiles[i].Content = strings.Replace(
			plan.GeneratedFiles[i].Content,
			"# port: 3000  # omit to auto-allocate from 8100-8200",
			fmt.Sprintf("port: %d", port),
			1,
		)
		break
	}
	plan.Instructions = append(plan.Instructions, fmt.Sprintf("✓ Reserved stable port %d for %s.", port, plan.ServiceName))
}

type application struct {
	ops       []fileOp
	unapplied []string
}

type fileOp struct {
	relPath    string
	path       string
	tmpPath    string
	mode       os.FileMode
	existed    bool
	before     []byte
	beforeMode os.FileMode
}

func prepareApplication(plan *Plan) (*application, error) {
	app := &application{}
	for _, file := range plan.GeneratedFiles {
		op, err := stageFile(plan.RepoPath, file.RelPath, []byte(file.Content), modeForGeneratedFile(file.RelPath))
		if err != nil {
			app.cleanup()
			return nil, err
		}
		app.ops = append(app.ops, op)
	}
	for _, patch := range plan.SourcePatches {
		path, err := safeJoin(plan.RepoPath, patch.RelPath)
		if err != nil {
			app.cleanup()
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				app.unapplied = append(app.unapplied, patch.RelPath)
				continue
			}
			app.cleanup()
			return nil, err
		}
		next, ok := replaceManagedBlock(string(data), patch.Block)
		if !ok {
			app.unapplied = append(app.unapplied, patch.RelPath)
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			app.cleanup()
			return nil, err
		}
		op, err := stageFile(plan.RepoPath, patch.RelPath, []byte(next), info.Mode().Perm())
		if err != nil {
			app.cleanup()
			return nil, err
		}
		app.ops = append(app.ops, op)
	}
	return app, nil
}

func stageFile(root, rel string, content []byte, mode os.FileMode) (fileOp, error) {
	path, err := safeJoin(root, rel)
	if err != nil {
		return fileOp{}, err
	}
	op := fileOp{relPath: rel, path: path, mode: mode}
	if data, err := os.ReadFile(path); err == nil {
		op.existed = true
		op.before = data
		if info, statErr := os.Stat(path); statErr == nil {
			op.beforeMode = info.Mode().Perm()
			if mode == 0 {
				op.mode = op.beforeMode
			}
		}
	} else if !os.IsNotExist(err) {
		return fileOp{}, err
	}
	if op.mode == 0 {
		op.mode = 0644
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fileOp{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".anito-setup-*")
	if err != nil {
		return fileOp{}, err
	}
	op.tmpPath = tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(op.tmpPath)
		return fileOp{}, err
	}
	if err := tmp.Chmod(op.mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(op.tmpPath)
		return fileOp{}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(op.tmpPath)
		return fileOp{}, err
	}
	return op, nil
}

func (app *application) commit(result *ApplyResult) error {
	var committed []fileOp
	for _, op := range app.ops {
		if err := os.Rename(op.tmpPath, op.path); err != nil {
			app.rollback(committed)
			app.cleanup()
			return err
		}
		committed = append(committed, op)
		if isPatchPath(result.Plan.SourcePatches, op.relPath) {
			result.AppliedPatches = append(result.AppliedPatches, op.relPath)
		} else {
			result.AppliedFiles = append(result.AppliedFiles, op.relPath)
		}
	}
	return nil
}

func (app *application) rollback(committed []fileOp) {
	for i := len(committed) - 1; i >= 0; i-- {
		op := committed[i]
		if op.existed {
			mode := op.beforeMode
			if mode == 0 {
				mode = op.mode
			}
			_ = os.WriteFile(op.path, op.before, mode)
		} else {
			_ = os.Remove(op.path)
		}
	}
}

func (app *application) cleanup() {
	for _, op := range app.ops {
		if op.tmpPath != "" {
			_ = os.Remove(op.tmpPath)
		}
	}
}

func validateRepoRoot(root string) error {
	if root == "" {
		return fmt.Errorf("repo_path is required")
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("repo_path %q is not a directory", root)
	}
	return nil
}

func validatePlanPaths(plan *Plan) error {
	seen := map[string]string{}
	for _, file := range plan.GeneratedFiles {
		key, err := validatedRelPath(plan.RepoPath, file.RelPath)
		if err != nil {
			return err
		}
		if prev := seen[key]; prev != "" {
			return fmt.Errorf("setup plan writes %q more than once (%s and generated file)", file.RelPath, prev)
		}
		seen[key] = "generated file"
	}
	for _, patch := range plan.SourcePatches {
		key, err := validatedRelPath(plan.RepoPath, patch.RelPath)
		if err != nil {
			return err
		}
		if prev := seen[key]; prev != "" {
			return fmt.Errorf("setup plan writes %q more than once (%s and source patch)", patch.RelPath, prev)
		}
		seen[key] = "source patch"
	}
	return nil
}

func validatedRelPath(root, rel string) (string, error) {
	path, err := safeJoin(root, rel)
	if err != nil {
		return "", err
	}
	return filepath.Clean(path), nil
}

func safeJoin(root, rel string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("repo_path is required")
	}
	if rel == "" {
		return "", fmt.Errorf("generated file path is required")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("generated path %q must be relative", rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	path := filepath.Clean(filepath.Join(absRoot, rel))
	prefix := absRoot + string(os.PathSeparator)
	if path != absRoot && !strings.HasPrefix(path, prefix) {
		return "", fmt.Errorf("generated path %q escapes repo root", rel)
	}
	return path, nil
}

func replaceManagedBlock(content, block string) (string, bool) {
	start := strings.Index(content, ManagedBlockStart)
	if start == -1 {
		return "", false
	}
	endRel := strings.Index(content[start:], ManagedBlockEnd)
	if endRel == -1 {
		return "", false
	}
	end := start + endRel + len(ManagedBlockEnd)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	replacement := block
	if !strings.HasSuffix(replacement, "\n") {
		replacement += "\n"
	}
	return content[:start] + replacement + content[end:], true
}

func modeForGeneratedFile(rel string) os.FileMode {
	if strings.HasSuffix(rel, ".sh") {
		return 0755
	}
	return 0644
}

func isPatchPath(patches []SourcePatch, rel string) bool {
	for _, patch := range patches {
		if patch.RelPath == rel {
			return true
		}
	}
	return false
}

func copyAllocations(in PortAllocation) PortAllocation {
	if len(in) == 0 {
		return nil
	}
	out := make(PortAllocation, len(in))
	for name, port := range in {
		out[name] = port
	}
	return out
}
