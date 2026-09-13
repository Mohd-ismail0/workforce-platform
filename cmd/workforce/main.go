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
	"workforce.local/platform/internal/api"
	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/store"
)

func main() {
	migrate := flag.Bool("migrate", false, "apply migrations")
	seed := flag.Bool("seed", false, "seed exactly two documented synthetic organizations and fixtures")
	worker := flag.Bool("worker", false, "run the River worker until shutdown")
	flag.Parse()
	ctx := context.Background()
	if *migrate {
		u := os.Getenv("MIGRATION_DATABASE_URL")
		if u == "" {
			log.Fatal("MIGRATION_DATABASE_URL is required")
		}
		if e := store.Migrate(ctx, u, "migrations"); e != nil {
			log.Fatal(e)
		}
		fmt.Println("migrations applied")
		return
	}
	if *seed {
		u := os.Getenv("DATABASE_URL")
		if u == "" {
			log.Fatal("DATABASE_URL is required")
		}
		st, e := store.Open(ctx, u)
		if e != nil {
			log.Fatal(e)
		}
		defer st.Close()
		if e := store.Seed(ctx, st); e != nil {
			log.Fatal(e)
		}
		fmt.Println("seeded org-fixture-a and org-fixture-b")
		return
	}
	cfg, e := platform.LoadConfig()
	if e != nil {
		log.Fatal(e)
	}
	st, e := store.Open(ctx, cfg.DatabaseURL)
	if e != nil {
		log.Fatal(e)
	}
	defer st.Close()

	if *worker {
		workerCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		if e := st.RunWorker(workerCtx, nil); e != nil && !errors.Is(e, context.Canceled) {
			log.Fatal(e)
		}
		return
	}
	log.Printf("workforce API listening on %s", cfg.Address)
	if e = http.ListenAndServe(cfg.Address, api.New(cfg, st).Handler()); e != nil {
		log.Fatal(e)
	}
}
