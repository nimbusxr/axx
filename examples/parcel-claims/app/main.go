// The claims service: shops claim for parcels that arrived damaged or never
// arrived. It runs on AWS: insured parcels and claims in DynamoDB, evidence
// photos and settlement letters in S3, and messages over SQS, SNS and
// EventBridge.
//
//	claims             serve the API and process the queues
//	claims provision   create the AWS resources it needs (what Terraform does in a real account)
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// openAPI describes the service's API.
//
//go:embed openapi.yaml
var openAPI []byte

// names are the AWS resources the service uses.
type names struct {
	InsuredParcels, Claims          string // DynamoDB tables
	Evidence, Letters, Reviews      string // S3 buckets
	EvidenceUploads, RefundRequests string // SQS queues
	RefundResults, CarrierDamage    string
	ParcelEvents                    string // the queue subscribed to the parcel-events topic
	Decisions, ParcelEventsTopic    string // SNS topics
	ClaimsBus, CarrierBus           string // EventBridge buses
}

var defaults = names{
	InsuredParcels: "insured-parcels", Claims: "claims",
	Evidence: "claim-evidence", Letters: "claim-letters", Reviews: "claim-reviews",
	EvidenceUploads: "evidence-uploads", RefundRequests: "refund-requests",
	RefundResults: "refund-results", CarrierDamage: "carrier-damage-reports",
	ParcelEvents: "claims-parcel-events", Decisions: "claim-decisions", ParcelEventsTopic: "parcel-events",
	ClaimsBus: "parcels", CarrierBus: "carrier-events",
}

// clients are the AWS clients of the service.
type clients struct {
	db  *dynamodb.Client
	s3  *s3.Client
	sqs *sqs.Client
	sns *sns.Client
	eb  *eventbridge.Client
}

func newClients(ctx context.Context) (clients, error) {
	// The SDK's usual configuration: AWS_REGION, credentials from the
	// environment or the instance role, and AWS_ENDPOINT_URL to use an
	// emulator instead of AWS.
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return clients{}, err
	}
	pathStyle := os.Getenv("AWS_ENDPOINT_URL") != ""
	return clients{
		db:  dynamodb.NewFromConfig(cfg),
		s3:  s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = pathStyle }),
		sqs: sqs.NewFromConfig(cfg),
		sns: sns.NewFromConfig(cfg),
		eb:  eventbridge.NewFromConfig(cfg),
	}, nil
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("claims failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := newClients(ctx)
	if err != nil {
		return fmt.Errorf("configure AWS: %w", err)
	}
	if len(os.Args) > 1 && os.Args[1] == "provision" {
		return provision(ctx, c, defaults, log)
	}

	limit, _ := strconv.ParseFloat(env("CLAIMS_AUTO_APPROVE_LIMIT", "250"), 64)
	svc := &service{aws: c, names: defaults, log: log, autoApproveLimit: limit, now: time.Now}
	if err := svc.resolveQueues(ctx); err != nil {
		return err
	}
	svc.startWorkers(ctx)

	srv := &http.Server{Addr: env("CLAIMS_ADDR", ":8500"), Handler: svc.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("claims service listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
