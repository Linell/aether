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
	SDK     string
	Env     []string
	mu      sync.Mutex
	running map[string]*exec.Cmd
	ctx     context.Context
}

func New(root string, reg Registry, env []string) *Supervisor {
	return &Supervisor{root: root, reg: reg, Env: env, running: make(map[string]*exec.Cmd)}
}

func (s *Supervisor) Root() string { return s.root }

func (s *Supervisor) Conjure(ctx context.Context, name string) error {
	dir, err := policy.ResolveWithin(s.root, name)
	if err != nil {
		return err
	}
	if err := Scaffold(ctx, dir, name, s.SDK, Exec); err != nil {
		return err
	}
	s.reconcileOne(ctx, name)
	return nil
}

func (s *Supervisor) Run(ctx context.Context, interval time.Duration) error {
	s.mu.Lock()
	s.ctx = ctx
	s.mu.Unlock()
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
	cmd := exec.CommandContext(s.processContext(ctx), "sh", "-c", m.Run)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Dir = dir
	cmd.Env = s.Env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("host: %s: start: %v", name, err)
		return
	}
	s.running[name] = cmd
	go s.awaitExit(name, cmd)
}

func (s *Supervisor) processContext(fallback context.Context) context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return fallback
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
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
			log.Printf("host: %s: signal: %v", name, err)
		}
	}
}
