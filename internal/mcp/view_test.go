package mcp

import (
	"testing"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

func TestToViewIncludesOperationalState(t *testing.T) {
	startedAt := time.Now().Add(-time.Minute)
	svc := &registry.Service{
		Name:               "operational-view",
		Type:               registry.TypeBinary,
		StablePorts:        map[string]int{"default": 8100},
		InternalPorts:      map[string]int{"default": 51000},
		ProxyBindAddress:   registry.DefaultProxyBindAddress,
		HealthCheck:        "/ready",
		HealthCheckTimeout: 45 * time.Second,
		DrainWindow:        3 * time.Second,
		WatchPaths:         []string{"/repo/src"},
		RestartPolicy:      "always",
		Status:             registry.StatusFailed,
		CrashAttempts:      5,
		GaveUp:             true,
		LastStartedAt:      startedAt,
		StartHistory: []registry.StartEvent{{
			StartedAt: startedAt,
			ExitCode:  1,
			Duration:  time.Second,
		}},
	}
	svc.NormalizePorts()

	view := toView(svc)
	if view.HealthCheck != "/ready" || view.HealthCheckTimeout != "45s" || view.DrainWindow != "3s" {
		t.Fatalf("health policy missing from view: %+v", view)
	}
	if view.RestartPolicy != "always" || len(view.WatchPaths) != 1 {
		t.Fatalf("restart policy missing from view: %+v", view)
	}
	if view.CrashAttempts != 5 || !view.GaveUp {
		t.Fatalf("crash state missing from view: %+v", view)
	}
	if !view.LastStartedAt.Equal(startedAt) || len(view.StartHistory) != 1 || view.StartHistory[0].ExitCode != 1 {
		t.Fatalf("start history missing from view: %+v", view)
	}
}

func TestMergeDeployInputPreservesOmittedRegisteredConfiguration(t *testing.T) {
	existing := &registry.Service{
		Type:               registry.TypeBinary,
		Args:               []string{"serve"},
		ProxyBindAddress:   "100.64.0.1",
		HealthCheckPort:    "http",
		EnvFile:            "/repo/.env",
		HealthCheck:        "/ready",
		WatchPaths:         []string{"/repo/src"},
		DrainWindow:        3 * time.Second,
		HealthCheckTimeout: 45 * time.Second,
		RestartPolicy:      "always",
		ConfigPath:         "/repo/.anito/config.yaml",
	}
	got := mergeDeployInput(deployInput{Name: "svc", Path: "/repo/bin/svc"}, existing)
	if got.Type != "binary" || len(got.Args) != 1 || got.EnvFile != "/repo/.env" || got.HealthCheck != "/ready" {
		t.Fatalf("partial redeploy lost registered config: %+v", got)
	}
	if got.DrainWindow != "3s" || got.HealthCheckTimeout != "45s" || got.RestartPolicy != "always" {
		t.Fatalf("partial redeploy lost policy: %+v", got)
	}
	if got.ConfigPath != existing.ConfigPath || got.ProxyBindAddress != existing.ProxyBindAddress || got.HealthCheckPort != "http" {
		t.Fatalf("partial redeploy lost provenance or ports: %+v", got)
	}
}

func TestMergeDeployInputReplaceConfigKeepsExplicitReplacement(t *testing.T) {
	in := deployInput{Name: "svc", Path: "/repo/bin/svc", ReplaceConfig: true}
	got := mergeDeployInput(in, &registry.Service{EnvFile: "/repo/.env", RestartPolicy: "always"})
	if got.EnvFile != "" || got.RestartPolicy != "" {
		t.Fatalf("replace_config unexpectedly merged existing values: %+v", got)
	}
}
