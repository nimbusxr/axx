// Package all lists every pack axx publishes, by name: for tests and tools
// that need all of them at once (the step reference, the skills). An axx
// for a project is built with the packs it lists, not with this package.
package all

import (
	"github.com/nimbusxr/axx/core"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
	awsdynamodb "github.com/nimbusxr/axx/packs/aws/dynamodb"
	awseventbridge "github.com/nimbusxr/axx/packs/aws/eventbridge"
	awss3 "github.com/nimbusxr/axx/packs/aws/s3"
	awssns "github.com/nimbusxr/axx/packs/aws/sns"
	awssqs "github.com/nimbusxr/axx/packs/aws/sqs"
	azureblob "github.com/nimbusxr/axx/packs/azure/blob"
	azureservicebus "github.com/nimbusxr/axx/packs/azure/servicebus"
	"github.com/nimbusxr/axx/packs/files"
	gcpbigquery "github.com/nimbusxr/axx/packs/gcp/bigquery"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
	gcpfirestore "github.com/nimbusxr/axx/packs/gcp/firestore"
	gcppubsub "github.com/nimbusxr/axx/packs/gcp/pubsub"
	gcpstorage "github.com/nimbusxr/axx/packs/gcp/storage"
	"github.com/nimbusxr/axx/packs/kafka"
	"github.com/nimbusxr/axx/packs/logs"
	"github.com/nimbusxr/axx/packs/mock"
	"github.com/nimbusxr/axx/packs/mongo"
	"github.com/nimbusxr/axx/packs/rest"
	sqlpack "github.com/nimbusxr/axx/packs/sql"
	weba11y "github.com/nimbusxr/axx/packs/web/a11y"
	webcore "github.com/nimbusxr/axx/packs/web/core"
	webcoverage "github.com/nimbusxr/axx/packs/web/coverage"
	weblighthouse "github.com/nimbusxr/axx/packs/web/lighthouse"
	webnetwork "github.com/nimbusxr/axx/packs/web/network"
	webscreenshots "github.com/nimbusxr/axx/packs/web/screenshots"
)

// Packs returns every published pack, keyed by its name.
func Packs() map[string]core.Pack {
	return map[string]core.Pack{
		"rest":             rest.Pack(),
		"mock":             mock.Pack(),
		"sql":              sqlpack.Pack(),
		"mongo":            mongo.Pack(),
		"kafka":            kafka.Pack(),
		"logs":             logs.Pack(),
		"files":            files.Pack(),
		"web-core":         webcore.Pack(),
		"web-screenshots":  webscreenshots.Pack(),
		"web-a11y":         weba11y.Pack(),
		"web-network":      webnetwork.Pack(),
		"web-lighthouse":   weblighthouse.Pack(),
		"web-coverage":     webcoverage.Pack(),
		"aws-core":         awscore.Pack(),
		"aws-s3":           awss3.Pack(),
		"aws-sqs":          awssqs.Pack(),
		"aws-sns":          awssns.Pack(),
		"aws-eventbridge":  awseventbridge.Pack(),
		"aws-dynamodb":     awsdynamodb.Pack(),
		"gcp-core":         gcpcore.Pack(),
		"gcp-storage":      gcpstorage.Pack(),
		"gcp-pubsub":       gcppubsub.Pack(),
		"gcp-bigquery":     gcpbigquery.Pack(),
		"gcp-firestore":    gcpfirestore.Pack(),
		"azure-blob":       azureblob.Pack(),
		"azure-servicebus": azureservicebus.Pack(),
	}
}
