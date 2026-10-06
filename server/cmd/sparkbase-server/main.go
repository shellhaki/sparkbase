// Command sparkbase-server runs the Sparkbase API, the worker, or both.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/shellhaki/sparkbase/server/internal/api"
	"github.com/shellhaki/sparkbase/server/internal/app"
	"github.com/shellhaki/sparkbase/server/internal/config"
	"github.com/shellhaki/sparkbase/server/internal/db"
	"github.com/shellhaki/sparkbase/server/internal/engine"
	"github.com/shellhaki/sparkbase/server/internal/logx"
	"github.com/shellhaki/sparkbase/server/internal/sbcrypto"
	"github.com/shellhaki/sparkbase/server/internal/store"
	"github.com/shellhaki/sparkbase/server/internal/worker"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("sparkbase-server", version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "rotate-encryption-key" {
		if err := rotateKey(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	log := logx.New(os.Stdout, cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, log, nil)
	if err != nil {
		return err
	}
	defer a.Close()
	if !a.Mail.Configured() {
		log.Warn("SMTP_URL is not set: sign-in links and emails are written to these logs")
	}

	var wg sync.WaitGroup
	if cfg.Mode == "all" || cfg.Mode == "api" {
		srv := api.New(a)
		if n, err := a.Store.CountUsers(ctx); err != nil {
			return err
		} else if n == 0 {
			// Until the first account exists, whoever signs up first becomes
			// the admin, so finish setup right after starting.
			fmt.Fprintf(os.Stdout, "\n  Sparkbase has no admin yet.\n  Open %s/setup now to create the admin account.\n\n", cfg.DashboardURL)
		}
		if a.Engine != nil {
			bind := "127.0.0.1"
			if a.Engine.InDocker {
				bind = "0.0.0.0"
			}
			act := &engine.Activator{Engine: a.Engine, Store: a.Store, Redis: a.Redis, Prefix: cfg.RedisPrefix, Log: log, BindHost: bind}
			wg.Add(1)
			go func() { defer wg.Done(); act.Run(ctx) }()
			log.Info("container engine on", "network", cfg.DockerNetwork, "inDocker", a.Engine.InDocker)
		}
		hs := &http.Server{
			Addr: ":" + strconv.Itoa(cfg.Port), Handler: srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("api listening", "port", cfg.Port, "url", cfg.PublicAPIURL, "version", version)
			if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("api stopped", "err", err.Error())
				stop()
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = hs.Shutdown(sctx)
		}()
	}
	if cfg.Mode == "all" || cfg.Mode == "worker" {
		w := worker.New(a)
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("worker started", "id", w.ID)
			w.Run(ctx)
			log.Info("worker stopped")
		}()
	}
	wg.Wait()
	return nil
}

// rotateKey re-encrypts every stored secret from ENCRYPTION_KEY to
// NEW_ENCRYPTION_KEY. Stop the server first, then start it with the new key.
func rotateKey() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(os.Getenv("NEW_ENCRYPTION_KEY"))
	if err != nil || len(raw) != 32 {
		return fmt.Errorf("NEW_ENCRYPTION_KEY must be 32 bytes encoded as base64 (openssl rand -base64 32)")
	}
	oldBox, err := sbcrypto.NewBox(cfg.EncryptionKey)
	if err != nil {
		return err
	}
	newBox, err := sbcrypto.NewBox(raw)
	if err != nil {
		return err
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	n, err := store.New(pool).Rekey(ctx, oldBox.Open, newBox.Seal)
	if err != nil {
		return fmt.Errorf("nothing was changed: %w", err)
	}
	fmt.Printf("re-encrypted %d secrets; set ENCRYPTION_KEY to the new key and start the server\n", n)
	return nil
}
