package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeroAlcBeer/bangou/checker"
	"github.com/zeroAlcBeer/bangou/committed"
	"github.com/zeroAlcBeer/bangou/config"
	"github.com/zeroAlcBeer/bangou/web"
)

// buildSHA is injected at build time via -ldflags "-X main.buildSHA=<sha>".
var buildSHA = "dev"

func main() {
	cfg := config.Parse()

	db, err := committed.NewSQLite(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Create pipeline registry and start all pipelines
	reg := web.NewRegistry(ctx, db)
	pipelines, err := db.ListPipelines(ctx)
	if err != nil {
		log.Fatalf("load pipelines: %v", err)
	}
	for _, p := range pipelines {
		if err := reg.StartPipeline(p); err != nil {
			log.Printf("warn: start pipeline %s: %v", p.Name, err)
		}
	}
	if len(pipelines) == 0 {
		log.Printf("no pipelines configured — create one via the web UI")
	}

	// aria2: global download monitor, distributes to matching pipelines
	reg.SetupAria2(ctx, db)

	// Checker: periodic link health check
	go checker.Run(ctx, db, time.Hour)

	// Web server
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: web.NewServer(reg, db, buildSHA)}
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("web ui: http://%s", cfg.ListenAddr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// Startup failure (e.g. port in use) — nothing is in flight yet.
		log.Fatalf("listen: %v", err)
	case <-ctx.Done():
	}

	// Graceful shutdown: drain HTTP, then wait for in-flight merges so
	// mkvmerge is not killed mid-write. Requires a matching container
	// stop_grace_period — see docker-compose.yml.sample.
	log.Printf("shutdown: draining http")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: http: %v", err)
	}
	log.Printf("shutdown: waiting for in-flight tasks")
	if !reg.WaitTasks(14 * time.Minute) {
		log.Printf("shutdown: warn: tasks still running at deadline, exiting anyway")
	}
	log.Printf("shutdown: done")
}
