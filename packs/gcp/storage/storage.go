// Package gcpstorage is the gcp-storage pack: files uploaded to Cloud
// Storage buckets and the objects the services under test write there.
package gcpstorage

import (
	"context"
	"errors"
	"io"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

const name = "gcp-storage"

const packDoc = `Upload files to Cloud Storage buckets and check the objects your services write there.

The steps use the scenario's project (` + "`the {word} gcp project with the following properties:`" + `, from gcp-core). Checks wait for the object (10 seconds unless ` + "`within {duration}`" + ` says otherwise), since services write asynchronously: an upload that triggers processing (a Pub/Sub notification or an Eventarc trigger, say) and the object that processing writes.`

// Pack returns the gcp-storage pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      name,
		Namespace: name,
		Doc:       packDoc,
		Requires:  []string{gcpcore.Name},
		Steps: cloudstep.Objects{
			Pack: name, Container: "gcs bucket", Object: "object", Example: "carrier-invoices",
			Store: store,
		}.Steps(),
	}
}

func store(sc *core.Scenario) (cloudstep.ObjectStore, error) {
	p, err := gcpcore.Default(sc)
	if err != nil {
		return nil, err
	}
	c, err := gcpcore.Client(sc.Context(), sc.Suite(), name, p, func(ctx context.Context) (*storage.Client, error) {
		return storage.NewClient(ctx, p.REST("/storage/v1/")...)
	})
	if err != nil {
		return nil, err
	}
	return bucketStore{c}, nil
}

type bucketStore struct{ c *storage.Client }

func (b bucketStore) Put(ctx context.Context, bucket, object string, body []byte, contentType string) error {
	w := b.c.Bucket(bucket).Object(object).NewWriter(ctx)
	w.ContentType = contentType
	if _, err := w.Write(body); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (b bucketStore) Get(ctx context.Context, bucket, object string) ([]byte, bool, error) {
	r, err := b.c.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer r.Close()
	body, err := io.ReadAll(r)
	return body, true, err
}

func (b bucketStore) List(ctx context.Context, bucket string, max int) ([]string, error) {
	var names []string
	it := b.c.Bucket(bucket).Objects(ctx, nil)
	for len(names) < max {
		o, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		names = append(names, o.Name)
	}
	return names, nil
}
