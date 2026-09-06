package host

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/linell/aether/internal/policy"
)

type Supervisor struct {
	root    string
	reg     Registry
	mu      sync.Mutex
	running map[string]*exec.Cmd
}

func New(root string, reg Registry) *Supervisor {
	return &Supervisor{root: root, reg: reg, running: make(map[string]*exec.Cmd)}
}

func (s *Supervisor) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := s.Reconcile(ctx); err != nil {
			log.Printf("host: reconcile: %v", err)
		}
		select {
		case <-ctx.Done():
			s.Stop()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) Reconcile(ctx context.Context) error {
	daemons, err := s.reg.Daemons(ctx)
	if err != nil {
		return err
	}
	for _, d := range daemons {
		s.reconcileOne(ctx, d.Name)
	}
	return nil
}

func (s *Supervisor) reconcileOne(ctx context.Context, name string) {
	if s.isRunning(name) {
		return
	}
	dir, err := policy.ResolveWithin(s.root, name)
	if err != nil {
		log.Printf("host: %s: resolve dir: %v", name, err)
		return
	}
	m, err := ReadManifest(dir)
	if err != nil {
		log.Printf("host: %s: read manifest: %v", name, err)
		return
	}
	s.spawn(ctx, name, dir, m)
}

func (s *Supervisor) isRunning(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[name]
	return ok
}

func (s *Supervisor) spawn(ctx context.Context, name, dir string, m Manifest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.running[name]; ok {
		return
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", m.Run)
	cmd.Dir = dir
	cmd.Env = policy.ScrubEnv(os.Environ(), policy.DefaultEnvAllow)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("host: %s: start: %v", name, err)
		return
	}
	s.running[name] = cmd
	go s.awaitExit(name, cmd)
}

func (s *Supervisor) awaitExit(name string, cmd *exec.Cmd) {
	log.Printf("host: %s: exited: %v", name, cmd.Wait())
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[name] == cmd {
		delete(s.running, name)
	}
}

func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, cmd := range s.running {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("host: %s: signal: %v", name, err)
		}
	}
}
