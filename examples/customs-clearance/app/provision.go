package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
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
		if _, err := c.admin.CreateQueue(ctx, q, nil); err != nil && !exists(err) {
			return fmt.Errorf("queue %s: %w", q, err)
		}
	}
	for _, t := range []string{n.Events, n.Border} {
		if _, err := c.admin.CreateTopic(ctx, t, nil); err != nil && !exists(err) {
			return fmt.Errorf("topic %s: %w", t, err)
		}
	}
	if _, err := c.admin.CreateSubscription(ctx, n.Border, n.BorderSub, nil); err != nil && !exists(err) {
		return fmt.Errorf("subscription %s/%s: %w", n.Border, n.BorderSub, err)
	}
	log.Info("provisioned", "containers", 3, "queues", 2, "topics", 2, "subscriptions", 1)
	return nil
}

// waitForAzure waits until Blob Storage answers (an emulator starting next
// to the service).
func waitForAzure(ctx context.Context, c *clients) error {
	deadline := time.Now().Add(time.Minute)
	for {
		pager := c.blob.NewListContainersPager(nil)
		_, err := pager.NextPage(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// exists reports a Service Bus entity that exists already.
func exists(err error) bool {
	var re *azcore.ResponseError
	return errors.As(err, &re) && re.StatusCode == http.StatusConflict
}
