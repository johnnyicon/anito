package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/johnnyicon/anito/internal/registry"
)

// Manager supervises running processes
type Manager struct {
	mu      sync.RWMutex
	procs   map[string]*exec.Cmd
	logDir  string
	reg     *registry.Registry
}

func New(logDir string, reg *registry.Registry) (*Manager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{
		procs:  make(map[string]*exec.Cmd),
		logDir: logDir,
		reg:    reg,
	}, nil
}

// Start launches a service process
func (m *Manager) Start(svc *registry.Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, running := m.procs[svc.Name]; running {
		return fmt.Errorf("service %q is already running", svc.Name)
	}

	cmd, err := m.buildCmd(svc)
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		_ = m.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
		return fmt.Errorf("failed to start %q: %w", svc.Name, err)
	}

	m.procs[svc.Name] = cmd
	_ = m.reg.UpdateStatus(svc.Name, registry.StatusRunning, cmd.Process.Pid)

	// watch for unexpected exit
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		delete(m.procs, svc.Name)
		m.mu.Unlock()
		_ = m.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
	}()

	return nil
}

// Stop signals a service process to shut down
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cmd, ok := m.procs[name]
	if !ok {
		return fmt.Errorf("service %q is not running", name)
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	delete(m.procs, name)
	return m.reg.UpdateStatus(name, registry.StatusStopped, 0)
}

// Restart stops then starts a service
func (m *Manager) Restart(svc *registry.Service) error {
	// ignore stop error — it may not be running
	_ = m.Stop(svc.Name)
	return m.Start(svc)
}

// IsRunning returns true if the process is active
func (m *Manager) IsRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.procs[name]
	return ok
}

func (m *Manager) buildCmd(svc *registry.Service) (*exec.Cmd, error) {
	var cmd *exec.Cmd

	switch svc.Type {
	case registry.TypeBinary:
		cmd = exec.Command(svc.BinaryPath)
	case registry.TypeStatic:
		// Anito's own static wrapper binary — resolved relative to our executable
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		wrapper := filepath.Join(filepath.Dir(self), "anito-static")
		cmd = exec.Command(wrapper, "--dir", svc.BinaryPath)
	default:
		return nil, fmt.Errorf("unknown service type %q", svc.Type)
	}

	// Pass port via environment
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(svc.Port))

	// Load optional env file
	if svc.EnvFile != "" {
		envVars, err := loadEnvFile(svc.EnvFile)
		if err != nil {
			return nil, fmt.Errorf("loading env file: %w", err)
		}
		cmd.Env = append(cmd.Env, envVars...)
	}

	// Log stdout/stderr to per-service log files
	outPath := filepath.Join(m.logDir, svc.Name+".log")
	logFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	return cmd, nil
}

// loadEnvFile reads a simple KEY=VALUE env file
func loadEnvFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var vars []string
	for _, line := range splitLines(string(data)) {
		if line == "" || line[0] == '#' {
			continue
		}
		vars = append(vars, line)
	}
	return vars, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
