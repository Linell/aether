package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linell/aether/internal/api"
	"github.com/linell/aether/internal/inngest"
	"github.com/linell/aether/internal/store"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		exitUsage()
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		exitUsage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: aether <command> [flags]

commands:
  serve     open the store and start the REST server
  version   print the aether version`)
}

func exitUsage() {
	usage()
	os.Exit(2)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "aether.db", "SQLite database path")
	listen := fs.String("listen", ":8080", "listen address")
	drainEvery := fs.Duration("drain-every", 5*time.Second, "outbox drain interval")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	handler, err := api.New(st, os.Getenv("AETHER_TOKEN"))
	if err != nil {
		return err
	}
	pub, err := newInngest()
	if err != nil {
		return err
	}
	if err := inngest.RegisterScheduler(pub, st); err != nil {
		return err
	}
	if _, err := inngest.Connect(ctx, pub, instanceID()); err != nil {
		return err
	}
	go store.RunDrain(ctx, st, pub, *drainEvery)
	log.Printf("aether listening on %s (db=%s)", *listen, *dbPath)
	return runServer(ctx, &http.Server{Addr: *listen, Handler: handler})
}

func newInngest() (*inngest.Client, error) {
	opts, err := inngest.OptionsFromEnv(inngest.AppID)
	if err != nil {
		return nil, err
	}
	return inngest.New(opts)
}

func instanceID() string {
	name, err := os.Hostname()
	if err != nil {
		return inngest.AppID
	}
	return name
}

func runServer(ctx context.Context, srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
