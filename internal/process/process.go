package process

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

const (
	drainTimeout = 5 * time.Second // time between SIGTERM and SIGKILL
)

// runningProc tracks a live process and the ephemeral port it is on.
type runningProc struct {
	cmd          *exec.Cmd
	internalPort int
}

// Manager supervises running processes.
type Manager struct {
	mu     sync.RWMutex
	procs  map[string]*runningProc
	logDir string
	reg    *registry.Registry
}

func New(logDir string, reg *registry.Registry) (*Manager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{
		procs:  make(map[string]*runningProc),
		logDir: logDir,
		reg:    reg,
	}, nil
}

// Start launches a service process on a free ephemeral port and returns that port.
// The caller is responsible for health-checking the process before swapping the proxy.
func (m *Manager) Start(svc *registry.Service) (internalPort int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, running := m.procs[svc.Name]; running {
		return 0, fmt.Errorf("service %q is already running", svc.Name)
	}

	port, err := freePort()
	if err != nil {
		return 0, fmt.Errorf("could not find free port for %q: %w", svc.Name, err)
	}

	cmd, err := m.buildCmd(svc, port)
	if err != nil {
		return 0, err
	}

	if err := cmd.Start(); err != nil {
		_ = m.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
		return 0, fmt.Errorf("failed to start %q: %w", svc.Name, err)
	}

	m.procs[svc.Name] = &runningProc{cmd: cmd, internalPort: port}
	_ = m.reg.UpdateStatus(svc.Name, registry.StatusRunning, cmd.Process.Pid)
	_ = m.reg.UpdateInternalPort(svc.Name, port)

	// Watch for unexpected exit.
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		delete(m.procs, svc.Name)
		m.mu.Unlock()
		_ = m.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
	}()

	return port, nil
}

// Stop sends SIGTERM to the named service, then SIGKILL after drainTimeout.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	rp, ok := m.procs[name]
	if ok {
		delete(m.procs, name)
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("service %q is not running", name)
	}

	return drainProc(rp.cmd)
}

// StopPID sends SIGTERM to an arbitrary PID (used when draining the old process
// after a hot-swap). The caller no longer holds a reference to the cmd.
func StopPID(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(drainTimeout):
		_ = proc.Signal(syscall.SIGKILL)
	}
}

// Restart stops the old process (if running) then starts a new one.
// Returns the new internal port.
func (m *Manager) Restart(svc *registry.Service) (int, error) {
	_ = m.Stop(svc.Name) // ignore "not running" errors
	return m.Start(svc)
}

// IsRunning returns true if the process is currently active.
func (m *Manager) IsRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.procs[name]
	return ok
}

// InternalPort returns the ephemeral port for a running service, or 0.
func (m *Manager) InternalPort(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rp, ok := m.procs[name]; ok {
		return rp.internalPort
	}
	return 0
}

// --- helpers ---

func (m *Manager) buildCmd(svc *registry.Service, port int) (*exec.Cmd, error) {
	if svc.Type != registry.TypeBinary {
		return nil, fmt.Errorf("buildCmd: static services do not run a process")
	}

	cmd := exec.Command(svc.BinaryPath)

	// Inject the ephemeral port — the process binds here, not on the stable port.
	cmd.Env = append(os.Environ(), "PORT="+strconv.Itoa(port))

	if svc.EnvFile != "" {
		envVars, err := loadEnvFile(svc.EnvFile)
		if err != nil {
			return nil, fmt.Errorf("loading env file: %w", err)
		}
		cmd.Env = append(cmd.Env, envVars...)
	}

	outPath := filepath.Join(m.logDir, svc.Name+".log")
	logFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	return cmd, nil
}

// freePort asks the OS for an available TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// drainProc sends SIGTERM and waits up to drainTimeout before SIGKILL.
func drainProc(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(drainTimeout):
		_ = cmd.Process.Signal(syscall.SIGKILL)
		<-done
	}
	return nil
}

// loadEnvFile reads a simple KEY=VALUE env file.
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
