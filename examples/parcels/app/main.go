// Command parcels is the system under test of the axx example: a parcel
// service with a REST API (OpenAPI 3.1), PostgreSQL storage, a manifest
// importer that writes each manifest's documents to an export folder, a
// MongoDB tracking read model fed by depot scans (Kafka events, and the
// scanners' MQTT messages), calls to a downstream address service,
// ParcelRegistered events on Kafka (Avro), label print jobs and the
// printers' reports on RabbitMQ (AMQP), the couriers' delivery
// confirmations and every tracking update on NATS, and a parcel assistant
// that asks a model (OpenAI's API).
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

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type config struct {
	Addr          string
	DBURL         string
	MongoURI      string
	MongoDB       string
	AddressURL    string
	AddressAPIKey string
	CourierURL    string
	RatingAddr    string // the partner carrier's rating service (gRPC)
	GRPCAddr      string // where the tracking API (gRPC) listens
	LabelSecret   string
	KafkaBrokers  []string
	RegistryURL   string
	EventsTopic   string
	ScansTopic    string
	// AMQPURL is RabbitMQ, where labels are printed; MQTTURL the broker the
	// depots' scanners publish to; NATSURL where couriers confirm deliveries
	// and tracking updates go.
	AMQPURL string
	MQTTURL string
	NATSURL string
	// CourierCallbackKey signs the courier's callbacks; ShopWebhookURL and
	// ShopWebhookKey are where the shops hear of deliveries and how the
	// service signs them; ShopTokenKey signs the shops' tokens, and
	// ShopClients are the shops' client credentials (shop:secret,...).
	// CourierTokenKey signs the tokens of the couriers' app, and Couriers
	// are who can sign in to it (id:name:pin,...).
	CourierCallbackKey string
	ShopWebhookURL     string
	ShopWebhookKey     string
	ShopTokenKey       string
	ShopClients        map[string]string
	CourierTokenKey    string
	Couriers           map[string]courierAccount
	// RedisURL is Valkey, where the service caches; SMTPAddr its mail server
	// (host:port), and MailFrom who its mail comes from.
	RedisURL     string
	SMTPAddr     string
	MailFrom     string
	PollInterval time.Duration
	// ExportDir is where the service writes the documents of the manifests
	// it imports, for the shops' systems to collect.
	ExportDir string
	// LogUDP (host:port) also sends every log line there, as one datagram:
	// the way many services ship logs to a collector.
	LogUDP string
	// ModelURL is the model the parcel assistant asks (OpenAI's API, or a
	// server that speaks it), Model the model it names, and ModelAPIKey its key.
	ModelURL    string
	Model       string
	ModelAPIKey string
	// PublicURL is where other agents reach the service (its A2A agent's
	// card names it); PartnerMCPURL and PartnerAgentURL are the partner
	// carrier's MCP server and A2A agent, which deliver beyond the EU.
	PublicURL       string
	PartnerMCPURL   string
	PartnerAgentURL string
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
		CourierURL:    strings.TrimRight(env("PARCELS_COURIER_URL", "http://localhost:8082"), "/"),
		RatingAddr:    env("PARCELS_RATING_ADDR", "localhost:8084"),
		GRPCAddr:      env("PARCELS_GRPC_ADDR", ":8410"),
		LabelSecret:   env("PARCELS_LABEL_SECRET", "example-label-secret"),
		RegistryURL:   strings.TrimRight(env("PARCELS_SCHEMA_REGISTRY_URL", "http://localhost:9081"), "/"),
		EventsTopic:   env("PARCELS_EVENTS_TOPIC", "parcel-events"),
		ScansTopic:    env("PARCELS_SCANS_TOPIC", "depot-scans"),
		AMQPURL:       env("PARCELS_AMQP_URL", "amqp://parcels:parcels@localhost:5672/"),
		MQTTURL:       env("PARCELS_MQTT_URL", "mqtt://localhost:1883"),
		NATSURL:       env("PARCELS_NATS_URL", "nats://localhost:4222"),
		// Keys for the example only; a real service reads them from its vault.
		CourierCallbackKey: env("PARCELS_COURIER_CALLBACK_KEY", "example-courier-callback-key"),
		ShopWebhookURL:     env("PARCELS_SHOP_WEBHOOK_URL", "http://localhost:8083/webhooks"),
		ShopWebhookKey:     env("PARCELS_SHOP_WEBHOOK_KEY", "whsec_ZXhhbXBsZS1zaG9wLXdlYmhvb2sta2V5"),
		ShopTokenKey:       env("PARCELS_SHOP_TOKEN_KEY", "example-shop-token-key"),
		ShopClients:        map[string]string{},
		CourierTokenKey:    env("PARCELS_COURIER_TOKEN_KEY", "example-courier-token-key"),
		Couriers:           map[string]courierAccount{},
		RedisURL:           env("PARCELS_REDIS_URL", "redis://localhost:6379/0"),
		SMTPAddr:           env("PARCELS_SMTP_ADDR", "localhost:1025"),
		MailFrom:           env("PARCELS_MAIL_FROM", "Parcels <no-reply@parcels.example>"),
		ExportDir:          env("PARCELS_EXPORT_DIR", "../infra/exports"),
		LogUDP:             env("PARCELS_LOG_UDP", ""),
		ModelURL:           env("PARCELS_MODEL_URL", "http://localhost:8086/v1"),
		Model:              env("PARCELS_MODEL", "gpt-4.1-mini"),
		ModelAPIKey:        env("PARCELS_MODEL_API_KEY", "example-model-key"),
		PublicURL:          strings.TrimRight(env("PARCELS_PUBLIC_URL", "http://localhost:8400"), "/"),
		PartnerMCPURL:      env("PARCELS_PARTNER_MCP_URL", "http://localhost:8087/mcp"),
		PartnerAgentURL:    strings.TrimRight(env("PARCELS_PARTNER_AGENT_URL", "http://localhost:8087"), "/"),
	}
	for _, pair := range strings.Split(env("PARCELS_SHOP_CLIENTS", "wisteria-way:wisteria-client-secret"), ",") {
		if shop, secret, ok := strings.Cut(strings.TrimSpace(pair), ":"); ok {
			c.ShopClients[shop] = secret
		}
	}
	for _, entry := range strings.Split(env("PARCELS_COURIERS", "CR-LEJ-12:Hanna Wolf:4711,CR-LEJ-14:Mia Krause:4711,CR-LEJ-15:Paul Richter:4711,CR-LEJ-16:Lea Schmidt:4711,CR-DRS-07:Jonas Keller:4711,CR-LEJ-21:Emil Hartmann:4711,CR-DRS-11:Nora Lange:4711"), ",") {
		if parts := strings.Split(strings.TrimSpace(entry), ":"); len(parts) == 3 {
			c.Couriers[parts[0]] = courierAccount{name: parts[1], pin: parts[2]}
		}
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
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		os.Exit(admin(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		os.Exit(mcpStdio())
	}
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
	updates, err := openTrackingUpdates(cctx, cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer updates.Close()
	shops, err := newShopWebhooks(cfg.ShopWebhookURL, cfg.ShopWebhookKey, log)
	if err != nil {
		return err
	}
	cache, err := openCache(cctx, cfg.RedisURL, log)
	if err != nil {
		return err
	}
	defer cache.Close()
	// A tracking view is cached until its parcel's summary changes.
	tracking.rebuilt = cache.dropTracking
	mail := &mailer{addr: cfg.SMTPAddr, from: cfg.MailFrom, log: log}
	rec := &recorder{tracking: tracking, updates: updates, store: store, shops: shops, mail: mail, log: log}
	if err := updates.confirmDeliveries(ctx, rec); err != nil {
		return err
	}
	scanners, err := openScanners(cctx, cfg.MQTTURL, store, rec, log)
	if err != nil {
		return err
	}
	defer scanners.Close()
	labels := labeler{secret: []byte(cfg.LabelSecret)}
	partner := &partnerCarrier{mcpURL: cfg.PartnerMCPURL, agentURL: cfg.PartnerAgentURL, http: &http.Client{Timeout: 15 * time.Second}}
	printing, err := openPrinting(cctx, cfg.AMQPURL, store, labels, log)
	if err != nil {
		return err
	}
	defer printing.Close()
	rating, err := openRating(cfg.RatingAddr)
	if err != nil {
		return err
	}
	defer rating.Close()

	svc := &service{
		store:    store,
		tracking: tracking,
		address:  &addressClient{base: cfg.AddressURL, apiKey: cfg.AddressAPIKey, http: &http.Client{Timeout: 5 * time.Second}},
		courier:  &courierClient{base: cfg.CourierURL, http: &http.Client{Timeout: 5 * time.Second}},
		rating:   rating,
		events:   events,
		printing: printing,
		labels:   labels,
		rec:      rec,
		cache:    cache,
		mail:     mail,
		log:      log,

		assistant: newAssistant(cfg, store, tracking, partner, log),
		mcp:       &parcelsMCP{store: store, tracking: tracking, labels: labels},
		agent:     &parcelsAgent{store: store, tracking: tracking, partner: partner, log: log},
		publicURL: cfg.PublicURL,

		courierKey:   []byte(cfg.CourierCallbackKey),
		shopTokenKey: []byte(cfg.ShopTokenKey),
		shopClients:  cfg.ShopClients,

		courierTokenKey: []byte(cfg.CourierTokenKey),
		couriers:        cfg.Couriers,
	}
	go svc.runImporter(ctx, cfg.PollInterval)
	go printing.run(ctx)
	go (&documents{store: store, dir: cfg.ExportDir, log: log}).run(ctx, cfg.PollInterval)
	go tracking.runProjector(ctx, cfg.PollInterval)
	go events.consumeScans(ctx, rec)

	stopTrackingAPI, err := serveTrackingAPI(ctx, cfg.GRPCAddr, &trackingAPI{store: store, tracking: tracking, log: log})
	if err != nil {
		return err
	}
	defer stopTrackingAPI()

	srv := &http.Server{Addr: cfg.Addr, Handler: svc.routes(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("parcels is ready", "addr", cfg.Addr, "grpc", cfg.GRPCAddr)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer scancel()
	return srv.Shutdown(sctx)
}

// mcpStdio serves the parcels MCP server over stdio (`parcels mcp`), for an
// assistant that runs it as a command. Its logs go to stderr: stdout is the
// protocol's.
func mcpStdio() int {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		return 2
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := openStore(ctx, cfg.DBURL, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		return 1
	}
	defer store.Close()
	tracking, err := openTracking(ctx, cfg.MongoURI, cfg.MongoDB, log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		return 1
	}
	defer tracking.Close(context.WithoutCancel(ctx))
	m := &parcelsMCP{store: store, tracking: tracking, labels: labeler{secret: []byte(cfg.LabelSecret)}}
	if err := m.server().Run(ctx, &sdk.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "parcels:", err)
		return 1
	}
	return 0
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
