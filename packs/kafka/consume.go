package kafka

import (
	"fmt"
	"time"

	"github.com/nimbusxr/axx/core"
)

// consume adds expectations to a label and waits for a record of the topic
// that meets all of them (KafkaConsumerStepsBase.setupMessageMatchers).
// distinctHeaders is the "headers match" step's variant of the header check.
// The topic is argument 0 and the label argument 1 of every consumer step.
func consume(sc *core.Scenario, a core.Args, svcArg int, distinctHeaders bool, add func(*label) error) error {
	tc, err := topicClient(sc, a, 0, svcArg)
	if err != nil {
		return err
	}
	l := tc.label(a.String(1))
	tc.mu.Lock()
	err = add(l)
	tc.mu.Unlock()
	if err != nil {
		return err
	}
	cfg, err := configOf(sc.Suite())
	if err != nil {
		return err
	}
	return tc.await(sc, l, distinctHeaders, cfg.Timeout)
}

const keepMisses = describeRecords

// await scans the topic's records, oldest first, until one satisfies every
// expectation of the label, waiting for new records until timeout.
func (tc *TopicClient) await(sc *core.Scenario, l *label, distinctHeaders bool, timeout time.Duration) error {
	ctx := sc.Context()
	tc.mu.Lock()
	matchers := l.matchers()
	tc.mu.Unlock()
	report := &checkReport{Service: tc.Service.Name, Topic: tc.Topic, Label: l.Name, Matchers: matchers}
	st := stateKey.Of(sc)
	st.mu.Lock()
	st.last = report
	st.mu.Unlock()

	var floor map[int32]int64
	if tc.consumer.OffsetReset == "latest" {
		ends, err := endOffsets(ctx, tc.tailer.cl, tc.Topic)
		if err != nil {
			return fmt.Errorf("reading the end offsets of %s (consumer.auto.offset.reset=latest): %w", tc.Topic, err)
		}
		floor = ends
	}
	start := time.Now()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var (
		from   int64
		misses []recordInfo
	)
	for {
		recs, next, wake := tc.tailer.since(from)
		for _, r := range recs {
			if f, ok := floor[r.Partition]; ok && r.Offset < f {
				continue
			}
			d := r.view(ctx, tc.deser)
			tc.mu.Lock()
			ok, why := l.eval(d, distinctHeaders)
			tc.mu.Unlock()
			report.Checked++
			if ok {
				ri := info(r, d, 0)
				tc.mu.Lock()
				l.Matched = &ri
				tc.mu.Unlock()
				report.Passed = true
				report.Waited = time.Since(start).Round(time.Millisecond).String()
				sc.Log("kafka event named %s matched %s partition %d offset %d", l.Name, tc.Topic, r.Partition, r.Offset)
				return nil
			}
			mi := info(r, d, 400)
			mi.Mismatch = why
			misses = append(misses, mi)
			if len(misses) > keepMisses {
				misses = misses[1:]
			}
		}
		from = next
		select {
		case <-wake:
			continue
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		break
	}
	tc.mu.Lock()
	l.Matched = nil // it no longer meets every expectation of the label
	tc.mu.Unlock()
	total, dropped, lastErr := tc.tailer.status()
	report.OnTopic, report.Dropped, report.Consumer = total, dropped, lastErr
	report.Waited = time.Since(start).Round(time.Millisecond).String()
	report.Recent = misses
	msg := fmt.Sprintf("No records found in topic %s matching the kafka event named %s within %s (%d record(s) checked)",
		tc.Topic, l.Name, timeout, report.Checked)
	if lastErr != "" {
		msg += "; the consumer reported: " + lastErr
	}
	var actual any = "no records on the topic"
	if len(misses) > 0 {
		last := misses[len(misses)-1]
		actual = fmt.Sprintf("latest record (partition %d offset %d): %s", last.Partition, last.Offset, last.Mismatch)
	}
	return core.Fail(msg, matchers, actual)
}
