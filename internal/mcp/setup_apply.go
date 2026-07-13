package mcp

import (
	"github.com/johnnyicon/anito/internal/setup"
)

type setupPorts struct {
	s *Server
}

func (p setupPorts) UsedPorts() map[int]bool {
	return p.s.svc.UsedPorts()
}

func (p setupPorts) StablePort(name string) (int, bool) {
	svc, err := p.s.svc.Status(name)
	if err != nil || svc == nil || svc.StablePort == 0 {
		return 0, false
	}
	return svc.StablePort, true
}

func (p setupPorts) Reserve(name string, preferredPort int) (int, error) {
	return p.s.svc.Reserve(name, preferredPort)
}

func (p setupPorts) Remove(name string) error {
	return p.s.svc.Remove(name)
}

func (s *Server) applySetup(plan *setup.Plan) (*setup.ApplyResult, error) {
	return setup.Apply(plan, setupPorts{s: s})
}
