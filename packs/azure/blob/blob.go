// Package azureblob is the azure-blob pack: files uploaded to Blob Storage
// containers and the blobs the services under test write there.
package azureblob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

const name = "azure-blob"

const packDoc = `Upload files to Blob Storage containers and check the blobs your services write there.

Register the storage account with ` + "`the {word} azure storage account with the following properties:`" + ` (the first account registered is the default), set up as the Azure SDK is set up for the real service:

| Property | |
| --- | --- |
| ` + "`connection string`" + ` | the account's connection string, e.g. ` + "`${env:AZURE_STORAGE_CONNECTION_STRING}`" + `, or a local emulator's |
| ` + "`url`" + ` | the account's blob endpoint (` + "`https://<account>.blob.core.windows.net`" + `), signed in with the Azure default credential chain (environment, workload identity, managed identity, Azure CLI) |

Checks wait for the blob (10 seconds unless ` + "`within {duration}`" + ` says otherwise), since services write asynchronously. Values expand ` + "`${env:..}`" + ` and ` + "`${sys:..}`" + `.`

// Pack returns the azure-blob pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	steps := []core.StepDef{{
		ID: name + ".account", Keyword: "Given", Arg: core.ArgTable, Since: "0.1.0",
		Expr:     "the {word} azure storage account with the following properties:",
		Doc:      "Register the storage account the blob steps talk to: `connection string`, or `url` with the Azure default credential chain.",
		Examples: []string{"Given the customs azure storage account with the following properties:"},
		Run: func(sc *core.Scenario, a core.Args) error {
			acct, err := parse(sc.Suite(), a.String(0), a.Table)
			if err != nil {
				return err
			}
			return accounts.Of(sc).Add(acct.name, acct)
		},
	}}
	steps = append(steps, cloudstep.Objects{
		Pack: name, Container: "blob container", Object: "blob", Example: "declarations",
		Store: store,
	}.Steps()...)
	return core.Manifest{Name: name, Namespace: name, Doc: packDoc, Steps: steps}
}

type account struct {
	name, connection, url string
}

func (a *account) key() string { return a.connection + "|" + a.url }

func parse(s *core.Suite, n string, t *core.Table) (*account, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	a := &account{name: n}
	for _, p := range pairs {
		v := strings.TrimSpace(s.Interpolate(p.Value))
		switch p.Key {
		case "connection string":
			a.connection = v
		case "url":
			a.url = strings.TrimRight(v, "/")
		default:
			return nil, fmt.Errorf("unknown azure storage account property %q (supported: connection string, url)", p.Key)
		}
	}
	if (a.connection == "") == (a.url == "") {
		return nil, fmt.Errorf(`give the azure storage account either a "connection string" or a "url"`)
	}
	return a, nil
}

var accounts = core.NewStateKey(name, func(*core.Scenario) *core.Services[*account] {
	return core.NewServices[*account]("Azure storage account",
		`No Azure storage account is registered in this scenario; register one with "the {word} azure storage account with the following properties:"`)
}, nil)

func store(sc *core.Scenario) (cloudstep.ObjectStore, error) {
	a, err := accounts.Of(sc).Default()
	if err != nil {
		return nil, err
	}
	c, err := core.Cached(sc.Suite(), name+"/client/"+a.key(), func() (*azblob.Client, error) {
		if a.connection != "" {
			return azblob.NewClientFromConnectionString(a.connection, nil)
		}
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, err
		}
		return azblob.NewClient(a.url, cred, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot connect to the %s azure storage account: %w", a.name, err)
	}
	return containerStore{c}, nil
}

type containerStore struct{ c *azblob.Client }

func (s containerStore) Put(ctx context.Context, cont, name string, body []byte, contentType string) error {
	_, err := s.c.UploadBuffer(ctx, cont, name, body, &azblob.UploadBufferOptions{
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: to.Ptr(contentType)},
	})
	return err
}

func (s containerStore) Get(ctx context.Context, cont, name string) ([]byte, bool, error) {
	out, err := s.c.DownloadStream(ctx, cont, name, nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound, bloberror.ContainerNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer out.Body.Close()
	var b bytes.Buffer
	_, err = io.Copy(&b, out.Body)
	return b.Bytes(), true, err
}

func (s containerStore) List(ctx context.Context, cont string, max int) ([]string, error) {
	var names []string
	p := s.c.NewListBlobsFlatPager(cont, &container.ListBlobsFlatOptions{MaxResults: to.Ptr(int32(max))})
	if p.More() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, b := range page.Segment.BlobItems {
			names = append(names, *b.Name)
		}
	}
	return names, nil
}
