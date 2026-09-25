// The customs service: it clears the parcels the company ships abroad. It
// runs on Azure: brokers' commercial invoices, clearance certificates and
// the archive in Blob Storage, and declarations, duties and events over
// Service Bus.
//
//	customs             serve and process the queues and subscriptions
//	customs provision   create the Azure resources it needs (what Bicep or Terraform does in a real subscription)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus/admin"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

// names are the Azure resources the service uses.
type names struct {
	Invoices, Clearances, Archive string // blob containers
	Filings, Duties               string // Service Bus queues
	Events, Border                string // Service Bus topics
	BorderSub                     string // the service's subscription to the border topic
}

var defaults = names{
	Invoices: "commercial-invoices", Clearances: "clearances", Archive: "customs-archive",
	Filings: "customs-filings", Duties: "duty-payments",
	Events: "customs-events", Border: "border-events", BorderSub: "customs",
}

// clients are the Azure clients of the service.
type clients struct {
	blob  *azblob.Client
	bus   *azservicebus.Client
	admin *admin.Client
}

// newClients configures the clients from connection strings, as for Azure.
// SERVICEBUS_MANAGEMENT_ENDPOINT is for emulators whose Service Bus
// management API is not on the namespace's host.
func newClients() (*clients, error) {
	c := &clients{}
	var err error
	if c.blob, err = azblob.NewClientFromConnectionString(os.Getenv("AZURE_STORAGE_CONNECTION_STRING"), nil); err != nil {
		return nil, fmt.Errorf("blob storage: %w", err)
	}
	cs := os.Getenv("SERVICEBUS_CONNECTION_STRING")
	if c.bus, err = azservicebus.NewClientFromConnectionString(cs, nil); err != nil {
		return nil, fmt.Errorf("service bus: %w", err)
	}
	var opts azcore.ClientOptions
	if ep := os.Getenv("SERVICEBUS_MANAGEMENT_ENDPOINT"); ep != "" {
		u, err := url.Parse(ep)
		if err != nil {
			return nil, err
		}
		opts.PerRetryPolicies = []policy.Policy{relocate{u}}
	}
	if c.admin, err = admin.NewClientFromConnectionString(cs, &admin.ClientOptions{ClientOptions: opts}); err != nil {
		return nil, fmt.Errorf("service bus management: %w", err)
	}
	return c, nil
}

// relocate sends management requests to the management endpoint.
type relocate struct{ base *url.URL }

func (r relocate) Do(req *policy.Request) (*http.Response, error) {
	u := req.Raw().URL
	u.Scheme, u.Host = r.base.Scheme, r.base.Host
	u.Path = strings.TrimRight(r.base.Path, "/") + u.Path
	req.Raw().Host = r.base.Host
	return req.Next()
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("customs failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c, err := newClients()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "provision" {
		return provision(ctx, c, defaults, log)
	}

	deMinimis, _ := strconv.ParseFloat(env("CUSTOMS_DE_MINIMIS", "150"), 64)
	dutyRate, _ := strconv.ParseFloat(env("CUSTOMS_DUTY_RATE", "0.20"), 64)
	svc := &service{azure: c, names: defaults, log: log, deMinimis: deMinimis, dutyRate: dutyRate, now: time.Now}
	svc.startReceivers(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: env("CUSTOMS_ADDR", ":8700"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Info("customs service listening", "addr", srv.Addr)
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
