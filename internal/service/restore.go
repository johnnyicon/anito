package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/johnnyicon/anito/internal/issues"
	"github.com/johnnyicon/anito/internal/registry"
)

const defaultRestoreMaxParallel = 4

// RestoreOutcome is the per-service result of startup reconciliation.
type RestoreOutcome string

const (
	RestoreSkipped  RestoreOutcome = "skipped"
	RestoreRunning  RestoreOutcome = "running"
	RestoreStatic   RestoreOutcome = "static"
	RestoreFailed   RestoreOutcome = "failed"
	RestoreOrphaned RestoreOutcome = "orphaned"
	RestoreBindFail RestoreOutcome = "bind_failed"
)

type RestoreAllOptions struct {
	MaxParallel int
	IssueSource string
}

type RestoreServiceResult struct {
	Name          string
	PriorStatus   registry.ServiceStatus
	Outcome       RestoreOutcome
	StablePorts   map[string]int
	InternalPorts map[string]int
	PID           int
	Duration      time.Duration
	Error         string
}

type RestoreAllResult struct {
	StartedAt   time.Time
	CompletedAt time.Time
	Phase       StartupPhase
	Total       int
	Targets     int
	Restored    int
	Failed      int
	Orphaned    int
	Skipped     int
	BindFailed  int
	Services    []RestoreServiceResult
}

type restoreHooks struct {
	afterBind          func(name string)
	beforeWorker       func(name string)
	beforeHealthCheck  func(name string)
	afterServiceResult func(RestoreServiceResult)
}

var testRestoreHooks restoreHooks

// RestoreAll reconciles persisted registry state after daemon startup.
func (s *Service) RestoreAll(ctx context.Context, opts RestoreAllOptions) (*RestoreAllResult, error) {
	all := s.reg.All()
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	maxParallel := opts.MaxParallel
	if maxParallel <= 0 {
		maxParallel = defaultRestoreMaxParallel
	}
	state := s.StartupState()
	startedAt := state.StartedAt
	if state.Phase != StartPhaseBindingListeners && state.Phase != StartPhaseReconciling {
		startedAt = time.Now()
		s.startup.begin(startedAt, len(all), maxParallel)
	} else {
		if !startedAt.IsZero() {
			maxParallel = state.MaxParallel
		}
	}

	result := &RestoreAllResult{
		StartedAt: startedAt,
		Phase:     StartPhaseBindingListeners,
		Total:     len(all),
		Services:  make([]RestoreServiceResult, 0, len(all)),
	}
	defer func() {
		result.CompletedAt = time.Now()
		result.Phase = StartPhaseReady
		s.startup.finish(result.CompletedAt, result)
		s.StartWatchers()
	}()

	type target struct {
		svc *registry.Service
		res RestoreServiceResult
	}
	var targets []target

	for _, svc := range all {
		res := baseRestoreResult(svc)
		if len(svc.StablePorts) == 0 && svc.StablePort == 0 {
			res.Outcome = RestoreSkipped
			s.recordRestoreResult(result, res)
			continue
		}

		svc.NormalizePorts()
		if err := s.prx.RegisterPortsWithBind(svc.Name, svc.StablePorts, svc.ProxyBindAddress); err != nil {
			res.Outcome = RestoreBindFail
			res.Error = err.Error()
			_ = s.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
			s.recordStartupIssue(opts.IssueSource, "startup_bind", svc.Name, err, nil)
			log.Printf("[RESTORE_BIND_FAILED] name=%s error=%v", svc.Name, err)
			s.recordRestoreResult(result, res)
			continue
		}
		if testRestoreHooks.afterBind != nil {
			testRestoreHooks.afterBind(svc.Name)
		}

		if svc.Status != registry.StatusRunning {
			res.Outcome = RestoreSkipped
			s.recordRestoreResult(result, res)
			continue
		}
		targets = append(targets, target{svc: svc, res: res})
	}

	result.Targets = len(targets)
	if len(targets) == 0 {
		return result, nil
	}
	if maxParallel > len(targets) {
		maxParallel = len(targets)
	}
	s.startup.mu.Lock()
	s.startup.maxParallel = maxParallel
	s.startup.mu.Unlock()
	s.startup.setPhase(StartPhaseReconciling)
	result.Phase = StartPhaseReconciling

	jobs := make(chan target)
	results := make(chan RestoreServiceResult, len(targets))
	var wg sync.WaitGroup
	for i := 0; i < maxParallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				select {
				case <-ctx.Done():
					res := item.res
					res.Outcome = RestoreFailed
					res.Error = ctx.Err().Error()
					results <- res
					continue
				default:
				}
				results <- s.restoreOne(ctx, item.svc, item.res, opts.IssueSource)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, item := range targets {
			select {
			case <-ctx.Done():
				return
			case jobs <- item:
			}
		}
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		s.recordRestoreResult(result, res)
	}
	if err := ctx.Err(); err != nil {
		seen := make(map[string]bool, len(result.Services))
		for _, res := range result.Services {
			seen[res.Name] = true
		}
		for _, item := range targets {
			if seen[item.svc.Name] {
				continue
			}
			res := item.res
			res.Outcome = RestoreFailed
			res.Error = err.Error()
			s.recordStartupIssue(opts.IssueSource, "startup_canceled", item.svc.Name, err, nil)
			_ = s.reg.UpdateStatus(item.svc.Name, registry.StatusFailed, 0)
			s.recordRestoreResult(result, res)
		}
	}
	return result, nil
}

func baseRestoreResult(svc *registry.Service) RestoreServiceResult {
	return RestoreServiceResult{
		Name:        svc.Name,
		PriorStatus: svc.Status,
		StablePorts: copyPorts(svc.StablePorts),
	}
}

func (s *Service) restoreOne(ctx context.Context, svc *registry.Service, res RestoreServiceResult, issueSource string) RestoreServiceResult {
	start := time.Now()
	finish := func(r RestoreServiceResult) RestoreServiceResult {
		r.Duration = time.Since(start)
		return r
	}
	if testRestoreHooks.beforeWorker != nil {
		testRestoreHooks.beforeWorker(svc.Name)
	}

	unlock := s.lockDeploy(svc.Name)
	defer unlock()

	if svc.Type == registry.TypeStatic {
		if err := s.prx.SwapStatic(svc.Name, svc.BinaryPath); err != nil {
			return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_static", fmt.Errorf("static swap: %w", err)))
		}
		_ = s.reg.UpdateStatus(svc.Name, registry.StatusRunning, 0)
		res.Outcome = RestoreStatic
		return finish(res)
	}

	if _, err := os.Stat(svc.BinaryPath); err != nil {
		if os.IsNotExist(err) {
			res.Outcome = RestoreOrphaned
			res.Error = fmt.Sprintf("binary path missing: %s", svc.BinaryPath)
			_ = s.reg.UpdateStatus(svc.Name, registry.StatusOrphaned, 0)
			s.recordStartupIssue(issueSource, "startup_orphaned", svc.Name, errors.New(res.Error), nil)
			log.Printf("[ORPHAN] name=%s binary_path=%s", svc.Name, svc.BinaryPath)
			return finish(res)
		}
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_stat", fmt.Errorf("stat %s: %w", svc.BinaryPath, err)))
	}

	internalPorts, err := s.mgr.StartCandidate(svc)
	if err != nil {
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_start", err))
	}
	res.InternalPorts = copyPorts(internalPorts)

	if testRestoreHooks.beforeHealthCheck != nil {
		testRestoreHooks.beforeHealthCheck(svc.Name)
	}
	if err := ctx.Err(); err != nil {
		_ = s.mgr.StopFailed(svc.Name)
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_canceled", err))
	}

	hcTimeout := svc.HealthCheckTimeout
	if hcTimeout == 0 {
		hcTimeout = defaultHealthCheckTimeout
	}
	hcPort := healthCheckPort(svc, internalPorts)
	if err := waitHealthy(hcPort, svc.HealthCheck, hcTimeout); err != nil {
		_ = s.mgr.StopFailed(svc.Name)
		s.prx.UnswapPorts(svc.Name)
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_health", fmt.Errorf("health check: %w", err)))
	}

	if err := s.prx.SwapPorts(svc.Name, internalPorts); err != nil {
		_ = s.mgr.StopFailed(svc.Name)
		s.prx.UnswapPorts(svc.Name)
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_proxy", fmt.Errorf("proxy swap: %w", err)))
	}
	pid := s.mgr.PID(svc.Name)
	if pid == 0 {
		_ = s.mgr.StopFailed(svc.Name)
		s.prx.UnswapPorts(svc.Name)
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_pid", fmt.Errorf("service %q candidate exited before activation", svc.Name)))
	}
	_ = s.reg.UpdateStatus(svc.Name, registry.StatusRunning, pid)
	if err := s.mgr.Activate(svc.Name); err != nil {
		_ = s.mgr.StopFailed(svc.Name)
		s.prx.UnswapPorts(svc.Name)
		return finish(s.restoreFailed(res, svc.Name, issueSource, "startup_activate", err))
	}

	s.crashMu.Lock()
	delete(s.crashAttempts, svc.Name)
	s.crashMu.Unlock()
	_ = s.reg.UpdateCrashState(svc.Name, 0, false)

	res.Outcome = RestoreRunning
	res.PID = pid
	return finish(res)
}

func healthCheckPort(svc *registry.Service, internalPorts map[string]int) int {
	if p, ok := internalPorts[svc.HealthCheckPort]; ok && svc.HealthCheckPort != "" {
		return p
	}
	if p, ok := internalPorts["default"]; ok {
		return p
	}
	for _, p := range internalPorts {
		return p
	}
	return svc.PrimaryInternalPort()
}

func (s *Service) restoreFailed(res RestoreServiceResult, serviceName, issueSource, tool string, err error) RestoreServiceResult {
	res.Outcome = RestoreFailed
	if err != nil {
		res.Error = err.Error()
	}
	_ = s.reg.UpdateStatus(serviceName, registry.StatusFailed, 0)
	s.recordStartupIssue(issueSource, tool, serviceName, err, nil)
	log.Printf("[RESTORE_FAILED] name=%s error=%v", serviceName, err)
	return res
}

func (s *Service) recordRestoreResult(result *RestoreAllResult, res RestoreServiceResult) {
	switch res.Outcome {
	case RestoreRunning, RestoreStatic:
		result.Restored++
	case RestoreFailed:
		result.Failed++
	case RestoreOrphaned:
		result.Orphaned++
	case RestoreSkipped:
		result.Skipped++
	case RestoreBindFail:
		result.BindFailed++
	}
	result.Services = append(result.Services, res)
	sort.Slice(result.Services, func(i, j int) bool { return result.Services[i].Name < result.Services[j].Name })
	s.startup.incrementCompleted()
	if testRestoreHooks.afterServiceResult != nil {
		testRestoreHooks.afterServiceResult(res)
	}
}

func (s *Service) recordStartupIssue(source, tool, serviceName string, err error, context any) {
	if s.iss == nil || err == nil {
		return
	}
	if source == "" {
		source = "daemon:restore_failed"
	}
	issue := issues.Issue{
		Source:   source,
		Tool:     tool,
		Input:    serviceName,
		Error:    fmt.Sprintf("service %s: %v", serviceName, err),
		Severity: "error",
	}
	if context != nil {
		issue.Context = fmt.Sprint(context)
	}
	if appendErr := s.iss.Append(issue); appendErr != nil {
		log.Printf("[ERROR] startup issue append failed: %v", appendErr)
	}
}

func cloneRestoreAllResult(in *RestoreAllResult) *RestoreAllResult {
	if in == nil {
		return nil
	}
	out := *in
	out.Services = make([]RestoreServiceResult, len(in.Services))
	for i, svc := range in.Services {
		out.Services[i] = svc
		out.Services[i].StablePorts = copyPorts(svc.StablePorts)
		out.Services[i].InternalPorts = copyPorts(svc.InternalPorts)
	}
	return &out
}
