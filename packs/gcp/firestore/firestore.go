// Package gcpfirestore is the gcp-firestore pack: documents seeded into
// Firestore and the documents the services under test write.
package gcpfirestore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/genproto/googleapis/type/latlng"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/jsonassert"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

const name = "gcp-firestore"

// maxDocs is how many documents of a collection a check reads.
const maxDocs = 5000

const packDoc = `Seed Firestore and check the documents your services write.

The steps use the scenario's project (` + "`the {word} gcp project with the following properties:`" + `, from gcp-core) and its default database.

A **seed** is a YAML or JSON file that maps collections to their documents, by ID (a collection can be nested: ` + "`shipments/SHP-1/scans`" + `):

` + "```yaml" + `
shipments:
  SHP-1001:
    carrier: KESTREL
    weightKg: 2.5
    agreedPrice: 3.38
` + "```" + `

**Checks** wait (10 seconds unless ` + "`within {duration}`" + ` says otherwise): for a document at a path (` + "`invoices/INV-2026-09-KESTREL`" + `) to have properties, or for a collection to have a document meeting every condition. Conditions and properties are ` + "`field | value`" + ` rows, with a dotted path into maps (` + "`totals.billed`" + `), compared as text: timestamps in RFC 3339, ` + "`null`" + ` for null and ` + "`undefined`" + ` for absent. A collection check reads up to 5,000 of its documents.`

// Pack returns the gcp-firestore pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      name,
		Namespace: name,
		Doc:       packDoc,
		Requires:  []string{gcpcore.Name},
		Steps: []core.StepDef{
			{
				ID: name + ".seed", Keyword: "Given", Since: "0.1.0",
				Expr:     "a {filepath} firestore seed",
				Doc:      "Write the documents of a seed file (resolved against `resources`): YAML or JSON mapping collections to documents by ID.",
				Examples: []string{"Given a seeds/shipments.yaml firestore seed"},
				Run:      seed,
			},
			{
				ID: name + ".document", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:     "[[within {duration} ]]the {word} firestore document has the following properties:",
				Doc:      "Wait (10s, or the given time) until the document at the path exists with every `field | value` property.",
				Examples: []string{"Then within 30s the invoices/INV-2026-09-KESTREL firestore document has the following properties:"},
				Run:      document,
			},
			{
				ID: name + ".collection", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:     "[[within {duration} ]]the {word} firestore collection has a document where:",
				Doc:      "Wait (10s, or the given time) until the collection has a document meeting every `field | value` row.",
				Examples: []string{"Then the disputes firestore collection has a document where:"},
				Run:      collection,
			},
		},
	}
}

func client(sc *core.Scenario) (*firestore.Client, error) {
	p, err := gcpcore.Default(sc)
	if err != nil {
		return nil, err
	}
	return gcpcore.Client(sc.Context(), sc.Suite(), name, p, func(ctx context.Context) (*firestore.Client, error) {
		return firestore.NewClient(ctx, p.ID, p.GRPC()...)
	})
}

func seed(sc *core.Scenario, a core.Args) error {
	file := a.String(0)
	s, err := cloudstep.ReadSeed(sc, file)
	if err != nil {
		return err
	}
	c, err := client(sc)
	if err != nil {
		return err
	}
	for _, coll := range s.Names {
		docs, ok := s.Items[coll].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: %s must map document IDs to their fields", file, coll)
		}
		for id, fields := range docs {
			m, ok := fields.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: document %s/%s is not an object", file, coll, id)
			}
			if _, err := c.Collection(coll).Doc(id).Set(sc.Context(), stored(m)); err != nil {
				return fmt.Errorf("%s: cannot write %s/%s: %w", file, coll, id, err)
			}
		}
		sc.Log("seeded %d firestore document(s) into %s", len(docs), coll)
	}
	return nil
}

// stored converts seed values for Firestore: JSON numbers become integers
// or doubles.
func stored(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = stored(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = stored(e)
		}
		return out
	}
	return v
}

func document(sc *core.Scenario, a core.Args) error {
	c, err := client(sc)
	if err != nil {
		return err
	}
	path := a.String(1)
	d := cloudstep.Wait(a, 0)
	return cloudstep.Poll(sc, d, func() (bool, string, error) {
		snap, err := c.Doc(path).Get(sc.Context())
		if status.Code(err) == codes.NotFound {
			return false, fmt.Sprintf("There is no %s firestore document after %s", path, d), nil
		}
		if err != nil {
			return false, "", fmt.Errorf("cannot read the %s firestore document: %w", path, err)
		}
		err = jsonassert.Properties(docJSON(snap.Data()), a.Table, false)
		switch {
		case err == nil:
			return true, "", nil
		case core.IsAssertion(err):
			return false, fmt.Sprintf("The %s firestore document did not have the properties within %s: %v", path, d, err), nil
		}
		return false, "", err
	})
}

func collection(sc *core.Scenario, a core.Args) error {
	rs, err := cloudstep.Conditions(a.Table)
	if err != nil {
		return err
	}
	c, err := client(sc)
	if err != nil {
		return err
	}
	coll := a.String(1)
	fetch := func() ([]string, error) {
		var out []string
		it := c.Collection(coll).Limit(maxDocs).Documents(sc.Context())
		defer it.Stop()
		for {
			snap, err := it.Next()
			if errors.Is(err, iterator.Done) {
				return out, nil
			}
			if err != nil {
				return nil, fmt.Errorf("cannot read the %s firestore collection: %w", coll, err)
			}
			out = append(out, docJSON(snap.Data()))
		}
	}
	return cloudstep.ExpectRecords(sc, cloudstep.Wait(a, 0), fetch, rs, -1, "document", "the "+coll+" firestore collection")
}

// docJSON renders a document's fields as JSON: timestamps in RFC 3339,
// references as their path, bytes in base64, geo points as lat and lng.
func docJSON(fields map[string]any) string {
	b, err := json.Marshal(plain(fields))
	if err != nil {
		return "{}"
	}
	return string(b)
}

func plain(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plain(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	case *firestore.DocumentRef:
		if _, rel, ok := strings.Cut(x.Path, "/documents/"); ok {
			return rel
		}
		return x.Path
	case *latlng.LatLng:
		return map[string]float64{"lat": x.GetLatitude(), "lng": x.GetLongitude()}
	}
	return v
}
