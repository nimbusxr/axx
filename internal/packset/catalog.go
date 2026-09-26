package packset

import "sort"

// Pack is a pack axx publishes: its name in axx-packs.yaml, the Go package
// that provides it, and the packs it builds on (loaded with it).
type Pack struct {
	Name     string
	Import   string
	Requires []string
	Summary  string
}

const packs = "github.com/nimbusxr/axx/packs/"

// Catalog lists the packs axx publishes. It names them only: a project's
// axx is built with the ones its axx-packs.yaml lists.
var Catalog = []Pack{
	{Name: "rest", Import: packs + "rest", Summary: "REST requests and responses, OpenAPI validation"},
	{Name: "mock", Import: packs + "mock", Summary: "WireMock services and their OpenAPI contracts"},
	{Name: "sql", Import: packs + "sql", Summary: "SQL databases: seeds, selections, locks, triggers"},
	{Name: "mongo", Import: packs + "mongo", Summary: "MongoDB: seeds and selections"},
	{Name: "kafka", Import: packs + "kafka", Summary: "Kafka events, Avro and the Schema Registry"},
	{Name: "logs", Import: packs + "logs", Summary: "the entries services log"},
	{Name: "files", Import: packs + "files", Summary: "the files services write to a folder"},
	{Name: "web-core", Import: packs + "web/core", Summary: "web apps in real browsers, driven by Playwright"},
	{Name: "web-screenshots", Import: packs + "web/screenshots", Requires: []string{"web-core"}, Summary: "pages that look as designed, compared with screenshots per platform"},
	{Name: "web-a11y", Import: packs + "web/a11y", Requires: []string{"web-core"}, Summary: "pages people can use whatever their abilities: axe-core audits and accessible structure"},
	{Name: "web-network", Import: packs + "web/network", Requires: []string{"web-core"}, Summary: "what pages fetch: failing, answered or slow requests, recordings, websocket messages"},
	{Name: "web-lighthouse", Import: packs + "web/lighthouse", Requires: []string{"web-core"}, Summary: "Lighthouse audits of pages: performance, accessibility, best practices and SEO scores, Core Web Vitals"},
	{Name: "web-coverage", Import: packs + "web/coverage", Requires: []string{"web-core"}, Summary: "how much of the web apps' JavaScript the scenarios run: lcov and Istanbul reports, from Chromium"},
	{Name: "aws-core", Import: packs + "aws/core", Summary: "the AWS account the aws-* packs use"},
	{Name: "aws-s3", Import: packs + "aws/s3", Requires: []string{"aws-core"}, Summary: "S3 buckets and objects"},
	{Name: "aws-sqs", Import: packs + "aws/sqs", Requires: []string{"aws-core"}, Summary: "SQS queues"},
	{Name: "aws-sns", Import: packs + "aws/sns", Requires: []string{"aws-core"}, Summary: "SNS topics"},
	{Name: "aws-eventbridge", Import: packs + "aws/eventbridge", Requires: []string{"aws-core"}, Summary: "EventBridge buses"},
	{Name: "aws-dynamodb", Import: packs + "aws/dynamodb", Requires: []string{"aws-core"}, Summary: "DynamoDB tables"},
	{Name: "gcp-core", Import: packs + "gcp/core", Summary: "the Google Cloud project the gcp-* packs use"},
	{Name: "gcp-storage", Import: packs + "gcp/storage", Requires: []string{"gcp-core"}, Summary: "Cloud Storage buckets and objects"},
	{Name: "gcp-pubsub", Import: packs + "gcp/pubsub", Requires: []string{"gcp-core"}, Summary: "Pub/Sub topics"},
	{Name: "gcp-bigquery", Import: packs + "gcp/bigquery", Requires: []string{"gcp-core"}, Summary: "BigQuery tables"},
	{Name: "gcp-firestore", Import: packs + "gcp/firestore", Requires: []string{"gcp-core"}, Summary: "Firestore documents"},
	{Name: "azure-blob", Import: packs + "azure/blob", Summary: "Blob Storage containers and blobs"},
	{Name: "azure-servicebus", Import: packs + "azure/servicebus", Summary: "Service Bus queues and topics"},
}

// Lookup returns a published pack by name.
func Lookup(name string) (Pack, bool) {
	for _, p := range Catalog {
		if p.Name == name {
			return p, true
		}
	}
	return Pack{}, false
}

// Names lists the published packs' names, sorted.
func Names() []string {
	out := make([]string, 0, len(Catalog))
	for _, p := range Catalog {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// WithRequired returns the entries with the published packs they require
// added before them, once each.
func WithRequired(entries []Entry) []Entry {
	seen := map[string]bool{}
	var out []Entry
	var add func(e Entry)
	add = func(e Entry) {
		if seen[e.Key()] {
			return
		}
		seen[e.Key()] = true
		if e.Kind == Published {
			if p, ok := Lookup(e.Name); ok {
				for _, r := range p.Requires {
					add(Entry{Raw: r, Kind: Published, Name: r})
				}
			}
		}
		out = append(out, e)
	}
	for _, e := range entries {
		add(e)
	}
	return out
}
