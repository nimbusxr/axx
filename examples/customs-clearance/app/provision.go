package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

// provision creates the Azure resources of the service, as the
// infrastructure code of a real subscription does. It can run again: what
// exists is kept.
func provision(ctx context.Context, c *clients, n names, log *slog.Logger) error {
	if err := waitForAzure(ctx, c); err != nil {
		return err
	}
	for _, cont := range []string{n.Invoices, n.Clearances, n.Archive} {
		if _, err := c.blob.CreateContainer(ctx, cont, nil); err != nil && !bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
			return fmt.Errorf("container %s: %w", cont, err)
		}
	}
	for _, q := range []string{n.Filings, n.Duties} {
		if err := ensure(ctx, "queue "+q,
			func() error { _, err := c.admin.CreateQueue(ctx, q, nil); return err },
			func() (bool, error) { r, err := c.admin.GetQueue(ctx, q, nil); return r != nil, err }); err != nil {
			return err
		}
	}
	for _, t := range []string{n.Events, n.Border} {
		if err := ensure(ctx, "topic "+t,
			func() error { _, err := c.admin.CreateTopic(ctx, t, nil); return err },
			func() (bool, error) { r, err := c.admin.GetTopic(ctx, t, nil); return r != nil, err }); err != nil {
			return err
		}
	}
	if err := ensure(ctx, "subscription "+n.Border+"/"+n.BorderSub,
		func() error { _, err := c.admin.CreateSubscription(ctx, n.Border, n.BorderSub, nil); return err },
		func() (bool, error) {
			r, err := c.admin.GetSubscription(ctx, n.Border, n.BorderSub, nil)
			return r != nil, err
		}); err != nil {
		return err
	}
	log.Info("provisioned", "containers", 3, "queues", 2, "topics", 2, "subscriptions", 1)
	return nil
}

// waitForAzure waits until Blob Storage and the Service Bus management API
// answer (emulators starting next to the service).
func waitForAzure(ctx context.Context, c *clients) error {
	deadline := time.Now().Add(provisionTimeout)
	for {
		_, err := c.blob.NewListContainersPager(nil).NextPage(ctx)
		if err == nil {
			_, err = c.admin.NewListQueuesPager(nil).NextPage(ctx)
		}
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(provisionRetry)
	}
}

// ensure creates a Service Bus entity until it exists. For a while after its
// management API answers, the emulator refuses to create entities, and it
// answers 409 Conflict both for an entity that exists and while another
// operation on it is in progress: only a look at the entity tells which.
func ensure(ctx context.Context, what string, create func() error, exists func() (bool, error)) error {
	deadline := time.Now().Add(provisionTimeout)
	for {
		err := create()
		if err == nil {
			return nil
		}
		if ok, gerr := exists(); gerr == nil && ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s: %w", what, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(provisionRetry):
		}
	}
}

// How long provisioning waits for the emulators, and between its tries.
var (
	provisionTimeout = 3 * time.Minute
	provisionRetry   = time.Second
)
