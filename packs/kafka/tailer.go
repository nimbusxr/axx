package kafka

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// tailer reads one topic from its first offset for the whole run, without a
// consumer group, and keeps the records in memory. Consumer assertions scan
// its records instead of joining a group each time, so every assertion sees
// the topic from the earliest offset without the cost of a rebalance.
type tailer struct {
	topic string
	cl    *kgo.Client
	max   int

	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	records []*record // records[i].seq == dropped + i
	dropped int64
	wake    chan struct{} // closed and replaced when records arrive
	lastErr error
	errAt   time.Time
}

// record is a raw record plus its decoded forms, cached per deserializer
// configuration.
type record struct {
	seq int64
	*kgo.Record

	mu    sync.Mutex
	views map[string]*decoded
}

func startTailer(topic string, opts []kgo.Opt, maxRecords int) (*tailer, error) {
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &tailer{topic: topic, cl: cl, max: maxRecords, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{})}
	go t.run(ctx)
	return t, nil
}

func (t *tailer) run(ctx context.Context) {
	defer close(t.done)
	for {
		fetches := t.cl.PollFetches(ctx)
		if ctx.Err() != nil || fetches.IsClientClosed() {
			return
		}
		var batch []*kgo.Record
		var fetchErr error
		fetches.EachError(func(_ string, p int32, err error) {
			if !errors.Is(err, context.Canceled) {
				fetchErr = fmt.Errorf("partition %d: %w", p, err)
			}
		})
		fetches.EachRecord(func(r *kgo.Record) { batch = append(batch, r) })
		t.add(batch, fetchErr)
	}
}

// add appends fetched records (and remembers a fetch error) and wakes the
// assertions waiting for records.
func (t *tailer) add(batch []*kgo.Record, fetchErr error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if fetchErr != nil {
		t.lastErr, t.errAt = fetchErr, time.Now()
	}
	if len(batch) == 0 {
		return
	}
	for _, r := range batch {
		t.records = append(t.records, &record{seq: t.dropped + int64(len(t.records)), Record: r})
	}
	if over := len(t.records) - t.max; t.max > 0 && over > 0 {
		t.records = append([]*record(nil), t.records[over:]...)
		t.dropped += int64(over)
	}
	close(t.wake)
	t.wake = make(chan struct{})
}

// since returns the records from sequence number from on, the sequence
// number after them, and a channel closed when more records arrive.
func (t *tailer) since(from int64) (recs []*record, next int64, wake <-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	start := max(from-t.dropped, 0)
	if start < int64(len(t.records)) {
		recs = append(recs, t.records[start:]...)
	}
	return recs, t.dropped + int64(len(t.records)), t.wake
}

// status describes the tailer for failure reports.
func (t *tailer) status() (count int64, dropped int64, lastErr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastErr != nil && time.Since(t.errAt) < time.Minute {
		lastErr = t.lastErr.Error()
	}
	return t.dropped + int64(len(t.records)), t.dropped, lastErr
}

func (t *tailer) close() {
	t.cancel()
	t.cl.Close()
	<-t.done
}
