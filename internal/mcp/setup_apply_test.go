package mcp

import (
	"reflect"
	"testing"

	"github.com/johnnyicon/anito/internal/setup"
)

func TestSetupPlanRequestMapsCompositeInput(t *testing.T) {
	in := setupInput{
		Path: "/repo",
		Services: []coordinateSvc{
			{Name: "api", Path: "/repo/api", PreferredPort: 8101},
			{Name: "web", Path: "/repo/web"},
		},
		Relationships: []coordinateRel{{From: "web", To: "api", ProxyPath: "/api"}},
	}

	got := toSetupPlanRequest(in)
	want := setup.PlanRequest{
		Path: "/repo",
		Services: []setup.ServiceSpec{
			{Name: "api", Path: "/repo/api", PreferredPort: 8101},
			{Name: "web", Path: "/repo/web"},
		},
		Relationships: []setup.Relationship{{From: "web", To: "api", ProxyPath: "/api"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestSetupResultMapsApplyResult(t *testing.T) {
	plan := &setup.Plan{
		Mode:           setup.ModeSingle,
		RepoPath:       "/repo",
		ServiceName:    "svc",
		Language:       setup.Go,
		HasPORT:        true,
		HasHealthRoute: true,
		Allocations:    setup.PortAllocation{"svc": 8100},
		GeneratedFiles: []setup.GeneratedFile{{RelPath: ".anito/config.yaml", Content: "name: svc\nport: 8100\n"}},
		SourcePatches:  []setup.SourcePatch{{RelPath: "vite.config.ts", Marker: "server", Block: "block", Instruction: "patch it"}},
		Instructions:   []string{"write config"},
	}
	applied := &setup.ApplyResult{
		Plan:             plan,
		Applied:          true,
		AppliedFiles:     []string{".anito/config.yaml"},
		AppliedPatches:   []string{"vite.config.ts"},
		UnappliedPatches: []string{"next.config.ts"},
	}

	got := toSetupResult(plan, applied)
	if got.Mode != "single" || got.RepoPath != "/repo" || got.ServiceName != "svc" {
		t.Fatalf("basic fields not mapped: %#v", got)
	}
	if got.Allocations["svc"] != 8100 {
		t.Fatalf("allocation = %v, want svc=8100", got.Allocations)
	}
	if len(got.GeneratedFiles) != 1 || got.GeneratedFiles[0].RelPath != ".anito/config.yaml" {
		t.Fatalf("generated files = %#v", got.GeneratedFiles)
	}
	if len(got.SourcePatches) != 1 || got.SourcePatches[0].RelPath != "vite.config.ts" {
		t.Fatalf("source patches = %#v", got.SourcePatches)
	}
	if !got.Applied {
		t.Fatal("Applied = false, want true")
	}
	if !reflect.DeepEqual(got.AppliedFiles, []string{".anito/config.yaml"}) {
		t.Fatalf("applied files = %v", got.AppliedFiles)
	}
	if !reflect.DeepEqual(got.AppliedPatches, []string{"vite.config.ts"}) {
		t.Fatalf("applied patches = %v", got.AppliedPatches)
	}
	if !reflect.DeepEqual(got.UnappliedPatches, []string{"next.config.ts"}) {
		t.Fatalf("unapplied patches = %v", got.UnappliedPatches)
	}
}
