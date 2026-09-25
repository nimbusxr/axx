// Command parcels is the system under test of the axx example: a parcel
// service with a REST API (OpenAPI 3.1), PostgreSQL storage, a manifest
// importer, a MongoDB tracking read model fed by depot scan events, calls to
// a downstream address service, and ParcelRegistered events on Kafka (Avro).
//
// Configuration comes from PARCELS_* environment variables. The defaults
// reach the infrastructure's published ports on localhost, which is what you
// want when running the service on the host; ../infra/compose.yaml points
// them at the compose services instead.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type config struct {
	Addr          string
	DBURL         string
	MongoURI      string
	MongoDB       string
	AddressURL    string
	AddressAPIKey string
	LabelSecret   string
	KafkaBrokers  []string
	RegistryURL   string
	EventsTopic   string
	ScansTopic    string
	PollInterval  time.Duration
	// LogUDP (host:port) also sends every log line there, as one datagram:
	// the way many services ship logs to a collector.
	LogUDP string
}

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return def
}

func loadConfig() (config, error) {
	c := config{
		Addr:          env("PARCELS_ADDR", ":8400"),
		DBURL:         env("PARCELS_DB_URL", "postgres://parcels:parcels@localhost:5432/parcels?sslmode=disable"),
		MongoURI:      env("PARCELS_MONGO_URI", "mongodb://parcels:parcels@localhost:27017/?authSource=admin"),
		MongoDB:       env("PARCELS_MONGO_DB", "parcels"),
		AddressURL:    strings.TrimRight(env("PARCELS_ADDRESS_URL", "http://localhost:8081"), "/"),
		AddressAPIKey: env("PARCELS_ADDRESS_API_KEY", "example-address-key"),
		LabelSecret:   env("PARCELS_LABEL_SECRET", "example-label-secret"),
		RegistryURL:   strings.TrimRight(env("PARCELS_SCHEMA_REGISTRY_URL", "http://localhost:9081"), "/"),
		EventsTopic:   env("PARCELS_EVENTS_TOPIC", "parcel-events"),
		ScansTopic:    env("PARCELS_SCANS_TOPIC", "depot-scans"),
		LogUDP:        env("PARCELS_LOG_UDP", ""),
	}
	for _, s := range strings.Split(env("PARCELS_KAFKA_BROKERS", "localhost:9092"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			c.KafkaBrokers = append(c.KafkaBrokers, s)
		}
	}
	var err error
	if c.PollInterval, err = time.ParseDuration(env("PARCELS_POLL_INTERVAL", "250ms")); err != nil {
		return c, fmt.Errorf("PARCELS_POLL_INTERVAL: %w", err)
	}
	return c, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(logOutput(cfg.LogUDP), &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = serve(ctx, cfg, log)
	stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, cfg config, log *slog.Logger) error {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
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
	events, err := openEvents(cctx, cfg, log)
	if err != nil {
		return err
	}
	defer events.Close()

	svc := &service{
		store:    store,
		tracking: tracking,
		address:  &addressClient{base: cfg.AddressURL, apiKey: cfg.AddressAPIKey, http: &http.Client{Timeout: 5 * time.Second}},
		events:   events,
		labels:   labeler{secret: []byte(cfg.LabelSecret)},
		log:      log,
	}
	go svc.runImporter(ctx, cfg.PollInterval)
	go tracking.runProjector(ctx, cfg.PollInterval)
	go events.consumeScans(ctx, tracking)

	srv := &http.Server{Addr: cfg.Addr, Handler: svc.routes(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("parcels is ready", "addr", cfg.Addr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer scancel()
	return srv.Shutdown(sctx)
}

// logOutput is stderr, and the UDP address when one is set.
func logOutput(udpAddr string) io.Writer {
	if udpAddr == "" {
		return os.Stderr
	}
	conn, err := (&net.Dialer{}).DialContext(context.Background(), "udp", udpAddr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parcels: logs are not sent to", udpAddr+":", err)
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, datagrams{conn})
}

// datagrams sends each write (one log line) as a datagram and never fails:
// losing a log line must not stop the service.
type datagrams struct{ conn net.Conn }

func (d datagrams) Write(p []byte) (int, error) {
	_, _ = d.conn.Write(p)
	return len(p), nil
}

// retry calls f until it succeeds or ctx ends, backing off up to 2s.
func retry(ctx context.Context, log *slog.Logger, what string, f func() error) error {
	delay := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		err := f()
		if err == nil {
			return nil
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Info("waiting for "+what, "err", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w (last error: %w)", what, ctx.Err(), err)
		case <-time.After(delay):
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}
