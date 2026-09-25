// Command parcels is the system under test of axx's agent evals: a small
// parcel-registration service with a REST API (OpenAPI 3.1), PostgreSQL
// storage, a MongoDB tracking read model, a manifest importer, calls to a
// downstream address service and ParcelRegistered events on Kafka (Avro).
//
// Usage:
//
//	parcels            serve (default)
//	parcels reset      wipe all test data: the Postgres schema, the Mongo
//	                   database, the address service's request journal and
//	                   the events topic
//	parcels version
//
// Configuration comes from PARCELS_* environment variables; the defaults
// match the evals environment (see config below). EVALS_MUTANT selects
// deliberate bugs (see evals/internal/mutant); only the verifier sets it.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nimbusxr/axx/evals/internal/mutant"
)

const version = "1.4.0"

type config struct {
	Addr           string
	DBURL          string
	MongoURI       string
	MongoDB        string
	AddressURL     string
	AddressAPIKey  string
	LabelSecret    string
	KafkaBrokers   []string
	RegistryURL    string
	EventsTopic    string
	PollInterval   time.Duration
	ConnectTimeout time.Duration
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return def
}

func loadConfig() (config, error) {
	c := config{
		Addr:          env("PARCELS_ADDR", ":8080"),
		DBURL:         env("PARCELS_DB_URL", "postgres://parcels:parcels@postgres:5432/parcels?sslmode=disable"),
		MongoURI:      env("PARCELS_MONGO_URI", "mongodb://parcels:parcels@mongo:27017/?authSource=admin"),
		MongoDB:       env("PARCELS_MONGO_DB", "parcels"),
		AddressURL:    strings.TrimRight(env("PARCELS_ADDRESS_URL", "http://address-service:8080"), "/"),
		AddressAPIKey: env("PARCELS_ADDRESS_API_KEY", "evals-address-key"),
		LabelSecret:   env("PARCELS_LABEL_SECRET", "evals-label-secret"),
		RegistryURL:   strings.TrimRight(env("PARCELS_SCHEMA_REGISTRY_URL", "http://schema-registry:8081"), "/"),
		EventsTopic:   env("PARCELS_EVENTS_TOPIC", "parcel-events"),
	}
	if b := env("PARCELS_KAFKA_BROKERS", ""); b != "" {
		for _, s := range strings.Split(b, ",") {
			if s = strings.TrimSpace(s); s != "" {
				c.KafkaBrokers = append(c.KafkaBrokers, s)
			}
		}
	}
	var err error
	if c.PollInterval, err = time.ParseDuration(env("PARCELS_POLL_INTERVAL", "250ms")); err != nil {
		return c, fmt.Errorf("PARCELS_POLL_INTERVAL: %w", err)
	}
	if c.ConnectTimeout, err = time.ParseDuration(env("PARCELS_CONNECT_TIMEOUT", "90s")); err != nil {
		return c, fmt.Errorf("PARCELS_CONNECT_TIMEOUT: %w", err)
	}
	return c, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		os.Exit(2)
	}
	if err := runCommand(cmd, cfg, log); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		os.Exit(1)
	}
}

func runCommand(cmd string, cfg config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch cmd {
	case "serve":
		return serve(ctx, cfg, log)
	case "reset":
		return reset(ctx, cfg, log)
	case "version", "--version":
		fmt.Println("parcels", version)
	case "help", "-h", "--help":
		fmt.Println("usage: parcels [serve|reset|version]")
	default:
		return fmt.Errorf("unknown command %q (usage: parcels [serve|reset|version])", cmd)
	}
	return nil
}

func serve(ctx context.Context, cfg config, log *slog.Logger) error {
	muts, unknown := mutant.Parse(os.Getenv(mutant.EnvVar))
	if len(unknown) > 0 {
		return fmt.Errorf("unknown value(s) in %s: %s", mutant.EnvVar, strings.Join(unknown, ", "))
	}
	// Keep the variable out of anything the service might start.
	_ = os.Unsetenv(mutant.EnvVar)

	cctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	store, err := openStore(cctx, cfg.DBURL, log)
	if err != nil {
		return err
	}
	defer store.Close()
	tracking, err := openTracking(cctx, cfg.MongoURI, cfg.MongoDB, log)
	if err != nil {
		return err
	}
	defer tracking.Close(context.WithoutCancel(ctx))
	var events *eventPublisher
	if len(cfg.KafkaBrokers) > 0 {
		events, err = newEventPublisher(cctx, cfg.KafkaBrokers, cfg.RegistryURL, cfg.EventsTopic, log)
		if err != nil {
			return err
		}
		defer events.Close()
	}

	svc := &service{
		store:    store,
		tracking: tracking,
		address:  &addressClient{base: cfg.AddressURL, apiKey: cfg.AddressAPIKey, http: &http.Client{Timeout: 5 * time.Second}},
		events:   events,
		labels:   labeler{secret: []byte(cfg.LabelSecret)},
		mut:      muts,
		log:      log,
	}
	go svc.runImporter(ctx, cfg.PollInterval)
	go tracking.runProjector(ctx, cfg.PollInterval, muts)

	srv := &http.Server{Addr: cfg.Addr, Handler: svc.routes(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("parcels is ready", "addr", cfg.Addr, "version", version, "events", events != nil)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer scancel()
	return srv.Shutdown(sctx)
}

// reset wipes every store the service and the tests write to.
func reset(ctx context.Context, cfg config, log *slog.Logger) error {
	cctx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	var errs []error
	if err := resetStore(cctx, cfg.DBURL); err != nil {
		errs = append(errs, fmt.Errorf("postgres: %w", err))
	}
	if err := resetTracking(cctx, cfg.MongoURI, cfg.MongoDB); err != nil {
		errs = append(errs, fmt.Errorf("mongo: %w", err))
	}
	if err := resetAddressJournal(cctx, cfg.AddressURL); err != nil {
		errs = append(errs, fmt.Errorf("address service: %w", err))
	}
	if len(cfg.KafkaBrokers) > 0 {
		if err := resetEvents(cctx, cfg.KafkaBrokers, cfg.EventsTopic); err != nil {
			errs = append(errs, fmt.Errorf("kafka: %w", err))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	log.Info("all test data removed")
	return nil
}
