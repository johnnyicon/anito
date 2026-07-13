// Package diagnosis provides a read-only, transport-neutral diagnosis result.
package diagnosis

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/johnnyicon/anito/internal/doctor"
	"github.com/johnnyicon/anito/internal/domain"
	"github.com/johnnyicon/anito/internal/registry"
)

type Request struct {
	ServiceName string `json:"service_name,omitempty"`
	RepoPath    string `json:"repo_path,omitempty"`
}

type Finding struct {
	Code     domain.Code `json:"code"`
	Severity string      `json:"severity"`
	Scope    string      `json:"scope,omitempty"`
	Field    string      `json:"field,omitempty"`
	Message  string      `json:"message"`
	Action   string      `json:"action,omitempty"`
}

type ServiceSnapshot struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Address string `json:"address,omitempty"`
	PID     int    `json:"pid,omitempty"`
	GaveUp  bool   `json:"gave_up,omitempty"`
}

type Result struct {
	Request  Request          `json:"request"`
	Healthy  bool             `json:"healthy"`
	Errors   int              `json:"errors"`
	Warnings int              `json:"warnings"`
	Findings []Finding        `json:"findings,omitempty"`
	Service  *ServiceSnapshot `json:"service,omitempty"`
}

type StatusFetcher interface {
	Status(name string) (*registry.Service, error)
}

func Run(req Request, svc StatusFetcher) (*Result, error) {
	req.ServiceName = strings.TrimSpace(req.ServiceName)
	req.RepoPath = strings.TrimSpace(req.RepoPath)
	if req.ServiceName == "" && req.RepoPath == "" {
		return nil, domain.InvalidConfigf("diagnosis requires service_name or repo_path")
	}

	result := &Result{Request: req, Healthy: true}
	if req.ServiceName != "" {
		diagnoseService(result, req.ServiceName, svc)
	}
	if req.RepoPath != "" {
		diagnoseRepo(result, req.RepoPath, svc)
	}
	result.Healthy = result.Errors == 0
	return result, nil
}

func diagnoseService(result *Result, name string, svc StatusFetcher) {
	if svc == nil {
		result.add(Finding{
			Code:     domain.CodeInvalidConfig,
			Severity: "warning",
			Scope:    "service",
			Message:  "daemon status is unavailable; service registry checks were skipped",
		})
		return
	}
	entry, err := svc.Status(name)
	if err != nil {
		if code, ok := domain.CodeOf(err); ok {
			result.add(Finding{Code: code, Severity: "error", Scope: "service", Message: err.Error()})
			return
		}
		result.add(Finding{Code: domain.CodeMissingService, Severity: "error", Scope: "service", Message: fmt.Sprintf("service %q not found", name)})
		return
	}
	result.Service = &ServiceSnapshot{
		Name:    entry.Name,
		Status:  string(entry.Status),
		Address: entry.Address(),
		PID:     entry.PID,
		GaveUp:  entry.GaveUp,
	}
	switch entry.Status {
	case registry.StatusFailed:
		severity := "error"
		action := fmt.Sprintf("redeploy or restart %s after fixing its health check failure", entry.Name)
		if entry.GaveUp {
			action = fmt.Sprintf("fix the crash loop, then redeploy or restart %s", entry.Name)
		}
		result.add(Finding{
			Code:     domain.CodeReadinessFailure,
			Severity: severity,
			Scope:    "service",
			Field:    "status",
			Message:  fmt.Sprintf("service %s is failed", entry.Name),
			Action:   action,
		})
	case registry.StatusOrphaned:
		result.add(Finding{
			Code:     domain.CodeInvalidConfig,
			Severity: "error",
			Scope:    "service",
			Field:    "binary_path",
			Message:  fmt.Sprintf("binary no longer exists on disk: %s", entry.BinaryPath),
			Action:   fmt.Sprintf("rebuild and redeploy, or remove %s from the registry", entry.Name),
		})
	}
}

func diagnoseRepo(result *Result, repoPath string, svc StatusFetcher) {
	abs, err := filepath.Abs(repoPath)
	if err == nil {
		repoPath = abs
	}
	report, err := doctor.Check(repoPath, svc)
	if err != nil {
		result.add(Finding{
			Code:     domain.CodeInvalidConfig,
			Severity: "error",
			Scope:    "repo",
			Message:  err.Error(),
			Action:   "run anito setup or correct the .anito config files",
		})
		return
	}
	for _, cfg := range report.Configs {
		scope := cfg.ConfigFile
		if cfg.ParseError != "" {
			result.add(Finding{
				Code:     domain.CodeInvalidConfig,
				Severity: "error",
				Scope:    scope,
				Field:    "config",
				Message:  cfg.ParseError,
				Action:   "fix the config file and rerun diagnosis",
			})
		}
		for _, issue := range cfg.Issues {
			result.add(Finding{
				Code:     classifyDoctorIssue(issue),
				Severity: issue.Severity,
				Scope:    scope,
				Field:    issue.Field,
				Message:  issue.Message,
				Action:   issue.Action,
			})
		}
	}
}

func classifyDoctorIssue(issue doctor.Issue) domain.Code {
	field := issue.Field
	switch {
	case field == "port" || strings.HasPrefix(field, "ports."):
		return domain.CodeConflict
	case field == "status":
		return domain.CodeReadinessFailure
	default:
		return domain.CodeInvalidConfig
	}
}

func (r *Result) add(f Finding) {
	if f.Severity == "" {
		f.Severity = "error"
	}
	f.Message = domain.Redact(f.Message)
	f.Action = domain.Redact(f.Action)
	r.Findings = append(r.Findings, f)
	switch f.Severity {
	case "error":
		r.Errors++
	case "warning":
		r.Warnings++
	}
}
