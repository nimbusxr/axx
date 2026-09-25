//go:build integration

package azureblob

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// The emulator accepts any account key; this one is "local" in base64.
const devKey = "bG9jYWw="

func TestBlobs(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci-az:latest", "4577", "", nil)
	h := cloudtest.New(t, Pack())
	h.OK("the customs azure storage account with the following properties:", [][]string{{
		"connection string",
		"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=" + devKey + ";BlobEndpoint=http://" + addr + "/devstoreaccount1;",
	}})
	st, err := store(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := st.(containerStore).c.CreateContainer(ctx, "declarations", nil); err != nil {
		t.Fatal(err)
	}

	h.File("declarations/DEC-1.json", `{"declaration":"DEC-1","goods":[{"hs":"610910","value":49.5}]}`)
	h.OK("the declarations/DEC-1.json file is uploaded to the declarations blob container as incoming/DEC-1.json")
	h.OK("the declarations blob container has a blob named incoming/DEC-1.json")
	h.OK("the incoming/DEC-1.json blob in the declarations blob container is identical to the declarations/DEC-1.json file")
	h.OK("the incoming/DEC-1.json blob in the declarations blob container has the following properties:", [][]string{
		{"declaration", "DEC-1"}, {"goods[0].hs", "610910"},
	})
	go func() {
		time.Sleep(time.Second)
		_ = st.Put(ctx, "declarations", "clearances/DEC-1.json", []byte(`{"cleared":true}`), "application/json")
	}()
	h.OK("within 5s the declarations blob container has a blob named clearances/DEC-1.json")
	err = h.Fails("within 1s the declarations blob container has a blob named clearances/DEC-2.json", "has no blob named clearances/DEC-2.json")
	if !strings.Contains(err.Error(), "incoming/DEC-1.json") {
		t.Errorf("the failure should list the container: %v", err)
	}
	h.Fails("the other azure storage account with the following properties:", `either a "connection string" or a "url"`, [][]string{{"url", "x"}, {"connection string", "y"}})
}
