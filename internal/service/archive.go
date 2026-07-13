package service

import (
	"github.com/johnnyicon/anito/internal/domain"
	"github.com/johnnyicon/anito/internal/registry"
)

// Archive marks a stopped/failed/orphaned registration as inactive without
// releasing its stable-port metadata. The entry can be restored later.
func (s *Service) Archive(name string) (*registry.Service, error) {
	if err := s.ensureMutable(); err != nil {
		return nil, err
	}
	current, ok := s.reg.Get(name)
	if !ok {
		return nil, domain.MissingServicef("service %q not found", name)
	}
	if current.Status == registry.StatusRunning {
		return nil, domain.Conflictf("service %q is running; stop it before archiving", name)
	}
	archived, err := s.reg.Archive(name)
	if err != nil {
		return nil, domain.Conflictf("cannot archive service %q: %v", name, err)
	}
	s.wtch.Stop(name)
	return archived, nil
}

// RestoreArchived returns an archived registration to the stopped state while
// preserving its stable ports and deployment metadata.
func (s *Service) RestoreArchived(name string) (*registry.Service, error) {
	if err := s.ensureMutable(); err != nil {
		return nil, err
	}
	if _, ok := s.reg.Get(name); !ok {
		return nil, domain.MissingServicef("service %q not found", name)
	}
	service, err := s.reg.RestoreArchived(name)
	if err != nil {
		return nil, domain.Conflictf("cannot restore archived service %q: %v", name, err)
	}
	return service, nil
}

// Prune permanently removes only an archived registration and leaves an
// auditable tombstone in the registry.
func (s *Service) Prune(name string) (registry.Tombstone, error) {
	if err := s.ensureMutable(); err != nil {
		return registry.Tombstone{}, err
	}
	current, ok := s.reg.Get(name)
	if !ok {
		return registry.Tombstone{}, domain.MissingServicef("service %q not found", name)
	}
	if current.Status != registry.StatusArchived {
		return registry.Tombstone{}, domain.Conflictf("service %q must be archived before pruning", name)
	}
	s.prx.RemovePorts(name)
	tomb, err := s.reg.Prune(name)
	if err != nil {
		return registry.Tombstone{}, domain.Conflictf("cannot prune service %q: %v", name, err)
	}
	return tomb, nil
}
