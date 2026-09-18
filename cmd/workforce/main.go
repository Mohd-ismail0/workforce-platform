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
	// Operator bootstrap. This is the ONLY onboarding step that runs outside the authenticated
	// API, because the first administrator has no linked external account yet and therefore no
	// way to authenticate. It is deliberately a local operator command rather than an HTTP
	// route, so it can never be an anonymous escalation path.
	bootstrap := flag.Bool("bootstrap-identity", false, "operator: create and link the FIRST administrator of an organization")
	bOrg := flag.String("org", "", "bootstrap: organization id")
	bOrgName := flag.String("org-name", "", "bootstrap: organization display name (defaults to the org id)")
	bPrincipal := flag.String("principal", "", "bootstrap: principal id")
	bPrincipalName := flag.String("principal-name", "", "bootstrap: principal display name (defaults to the principal id)")
	bRole := flag.String("role", "admin", "bootstrap: role for a NEW principal (requester|approver|admin)")
	bIssuer := flag.String("issuer", "", "bootstrap: exact issuer of the already-authenticated identity")
	bSubject := flag.String("subject", "", "bootstrap: exact subject of the already-authenticated identity")
	bActor := flag.String("actor", "", "bootstrap: the human operator doing this, recorded as the actor")
	flag.Parse()
	ctx := context.Background()
	if *bootstrap {
		u := os.Getenv("DATABASE_URL")
		if u == "" {
			log.Fatal("DATABASE_URL is required")
		}
		st, e := store.Open(ctx, u)
		if e != nil {
			log.Fatal(e)
		}
		defer st.Close()
		res, e := st.BootstrapIdentity(ctx, store.BootstrapRequest{
			OrgID: *bOrg, OrgName: *bOrgName,
			PrincipalID: *bPrincipal, PrincipalName: *bPrincipalName, Role: *bRole,
			Issuer: *bIssuer, Subject: *bSubject, Actor: *bActor,
		})
		if e != nil {
			log.Fatal(e)
		}
		// Deliberately prints no identifier beyond what the operator already supplied: this
		// output can end up in a shell history or a CI log.
		fmt.Printf("bootstrap: org_created=%v principal_created=%v linked=%v\n",
			res.OrgCreated, res.PrincipalCreated, res.Linked)
		return
	}
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
