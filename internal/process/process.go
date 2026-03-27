package process

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/johnnyicon/anito/internal/registry"
)

const (
	drainTimeout = 5 * time.Second // time between SIGTERM and SIGKILL
)

// runningProc tracks a live process and the ephemeral port(s) it is on.
type runningProc struct {
	cmd           *exec.Cmd
	internalPort  int            // primary port (backward compat)
	internalPorts map[string]int // all named ephemeral ports
	logFile       *os.File
	done          chan struct{} // closed by the Start goroutine when the process exits
}

// Manager supervises running processes.
type Manager struct {
	mu       sync.RWMutex
	procs    map[string]*runningProc
	draining map[int]bool // PIDs being intentionally killed; crash monitor ignores these
	logDir   string
	reg      *registry.Registry
	OnCrash  func(name string) // called when a process exits unexpectedly; may be nil
}

func New(logDir string, reg *registry.Registry) (*Manager, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}
	return &Manager{
		procs:    make(map[string]*runningProc),
		draining: make(map[int]bool),
		logDir:   logDir,
		reg:      reg,
	}, nil
}

// MarkDraining registers pid as an intentional kill so the crash monitor ignores it.
// Call this before StopPID when draining a process that is no longer in m.procs.
func (m *Manager) MarkDraining(pid int) {
	if pid <= 0 {
		return
	}
	m.mu.Lock()
	m.draining[pid] = true
	m.mu.Unlock()
}

// Start launches a service process on free ephemeral port(s) and returns them.
// For single-port services, returns a single-entry map {"default": port}.
// The caller is responsible for health-checking the process before swapping the proxy.
func (m *Manager) Start(svc *registry.Service) (map[string]int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, running := m.procs[svc.Name]; running {
		return nil, fmt.Errorf("service %q is already running", svc.Name)
	}

	// Allocate one ephemeral port per named port.
	ports := make(map[string]int, len(svc.StablePorts))
	if len(svc.StablePorts) > 0 {
		for name := range svc.StablePorts {
			p, err := freePort()
			if err != nil {
				return nil, fmt.Errorf("could not find free port for %q (port %s): %w", svc.Name, name, err)
			}
			ports[name] = p
		}
	} else {
		// Fallback for services without StablePorts set (shouldn't happen after normalization).
		p, err := freePort()
		if err != nil {
			return nil, fmt.Errorf("could not find free port for %q: %w", svc.Name, err)
		}
		ports["default"] = p
	}

	cmd, logFile, err := m.buildCmd(svc, ports)
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		_ = m.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
		return nil, fmt.Errorf("failed to start %q: %w", svc.Name, err)
	}

	primaryPort := svc.PrimaryInternalPort()
	if primaryPort == 0 {
		// Use the first port from our allocated set.
		for _, p := range ports {
			primaryPort = p
			break
		}
	}

	done := make(chan struct{})
	rp := &runningProc{cmd: cmd, internalPort: primaryPort, internalPorts: ports, logFile: logFile, done: done}
	m.procs[svc.Name] = rp
	_ = m.reg.UpdateInternalPorts(svc.Name, ports)

	// Watch for unexpected exit. This is the sole goroutine that calls cmd.Wait().
	pid := cmd.Process.Pid
	go func() {
		defer close(done)
		_ = cmd.Wait()
		m.mu.Lock()
		// Only delete our own entry — a re-deploy may have replaced it with a
		// new process under the same name.
		if current, ok := m.procs[svc.Name]; ok && current.cmd == cmd {
			delete(m.procs, svc.Name)
		}
		isDraining := m.draining[pid]
		delete(m.draining, pid)
		m.mu.Unlock()

		// Close this process's log file descriptor.
		if logFile != nil {
			_ = logFile.Close()
		}

		if isDraining {
			log.Printf("[DRAIN] name=%s pid=%d", svc.Name, pid)
			return // intentional kill — not a crash
		}

		_ = m.reg.UpdateStatus(svc.Name, registry.StatusFailed, 0)
		log.Printf("[CRASH] name=%s pid=%d", svc.Name, pid)
		if handler := m.OnCrash; handler != nil {
			handler(svc.Name)
		}
	}()

	return ports, nil
}

// Stop sends SIGTERM to the named service, then SIGKILL after drainTimeout.
func (m *Manager) Stop(name string) error {
	m.mu.Lock()
	rp, ok := m.procs[name]
	if ok {
		delete(m.procs, name)
		if rp.cmd.Process != nil {
			m.draining[rp.cmd.Process.Pid] = true
		}
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("service %q is not running", name)
	}

	return drainProc(rp.cmd, rp.done)
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
// Returns the new internal ports.
func (m *Manager) Restart(svc *registry.Service) (map[string]int, error) {
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

// Deregister removes name from the tracked process table without sending any
// signal, and returns the old PID, cmd, and done channel. Use this before
// starting a replacement process so the name slot is free; drain the returned
// cmd (via DrainProc) after the new process passes its health check.
// The done channel is closed by the Start goroutine when the process exits —
// pass it to DrainProc so it can wait without racing on cmd.Wait().
func (m *Manager) Deregister(name string) (int, *exec.Cmd, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rp, ok := m.procs[name]
	if !ok {
		return 0, nil, nil
	}
	delete(m.procs, name)
	if rp.cmd.Process != nil {
		return rp.cmd.Process.Pid, rp.cmd, rp.done
	}
	return 0, rp.cmd, rp.done
}

// PID returns the PID of a running process, or 0.
func (m *Manager) PID(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rp, ok := m.procs[name]; ok && rp.cmd.Process != nil {
		return rp.cmd.Process.Pid
	}
	return 0
}

// DrainProc sends SIGTERM to cmd and waits for it to exit via done (closed by
// the Start goroutine). Falls back to SIGKILL after drainTimeout.
// Exported so the service layer can drain a cmd returned by Deregister.
func DrainProc(cmd *exec.Cmd, done <-chan struct{}) error {
	return drainProc(cmd, done)
}

// InternalPort returns the primary ephemeral port for a running service, or 0.
func (m *Manager) InternalPort(name string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rp, ok := m.procs[name]; ok {
		return rp.internalPort
	}
	return 0
}

// InternalPorts returns all named ephemeral ports for a running service.
func (m *Manager) InternalPorts(name string) map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rp, ok := m.procs[name]; ok {
		return rp.internalPorts
	}
	return nil
}

// --- helpers ---

func (m *Manager) buildCmd(svc *registry.Service, ports map[string]int) (*exec.Cmd, *os.File, error) {
	if svc.Type != registry.TypeBinary {
		return nil, nil, fmt.Errorf("buildCmd: static services do not run a process")
	}

	cmd := exec.Command(svc.BinaryPath, svc.Args...)
	cmd.Env = os.Environ()

	// Inject ephemeral port env vars.
	isMultiPort := len(ports) > 1 || (len(ports) == 1 && !hasDefaultOnly(ports))
	if isMultiPort {
		// Multi-port: PORT_<NAME>=<ephemeral> for each named port.
		for name, p := range ports {
			envName := "PORT_" + strings.ToUpper(name)
			cmd.Env = append(cmd.Env, envName+"="+strconv.Itoa(p))
		}
		// Also set PORT to the health check port for backward compat with
		// frameworks that read PORT.
		hcPort := primaryPortFromMap(ports, svc.HealthCheckPort)
		cmd.Env = append(cmd.Env, "PORT="+strconv.Itoa(hcPort))
	} else {
		// Single-port: PORT=<ephemeral> (classic behavior).
		for _, p := range ports {
			cmd.Env = append(cmd.Env, "PORT="+strconv.Itoa(p))
		}
	}

	if svc.EnvFile != "" {
		envVars, err := loadEnvFile(svc.EnvFile)
		if err != nil {
			return nil, nil, fmt.Errorf("loading env file: %w", err)
		}
		cmd.Env = append(cmd.Env, envVars...)
	}

	outPath := filepath.Join(m.logDir, svc.Name+".log")
	logFile, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	return cmd, logFile, nil
}

func hasDefaultOnly(ports map[string]int) bool {
	if len(ports) != 1 {
		return false
	}
	_, ok := ports["default"]
	return ok
}

func primaryPortFromMap(ports map[string]int, healthCheckPort string) int {
	if healthCheckPort != "" {
		if p, ok := ports[healthCheckPort]; ok {
			return p
		}
	}
	if p, ok := ports["default"]; ok {
		return p
	}
	for _, p := range ports {
		return p
	}
	return 0
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

// drainProc sends SIGTERM to cmd and waits for the process to exit via done.
// done is the channel closed by the Start goroutine — this avoids calling
// cmd.Wait() a second time, which would race on exec.Cmd's internal goroutineErr
// channel and hang indefinitely.
func drainProc(cmd *exec.Cmd, done <-chan struct{}) error {
	if cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	if done == nil {
		// No done channel (process was not started via our Start()); fall back
		// to a timed SIGKILL with no wait.
		time.Sleep(drainTimeout)
		_ = cmd.Process.Signal(syscall.SIGKILL)
		return nil
	}
	select {
	case <-done:
	case <-time.After(drainTimeout):
		_ = cmd.Process.Signal(syscall.SIGKILL)
		<-done // Start goroutine will handle the actual wait
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
