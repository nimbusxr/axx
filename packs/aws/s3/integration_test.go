//go:build integration

package awss3

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

func TestObjects(t *testing.T) {
	addr := cloudtest.Emulator(t, "floci/floci:latest", "4566", "/_localstack/health", nil)
	h := cloudtest.New(t, awscore.Pack(), Pack())
	h.OK("the parcels aws account with the following properties:", [][]string{
		{"region", "eu-west-1"},
		{"endpoint", "http://" + addr},
		{"access key id", "test"},
		{"secret access key", "test"},
	})
	st, err := store(h.SC)
	if err != nil {
		t.Fatal(err)
	}
	client := st.(bucketStore).c
	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("claim-evidence")}); err != nil {
		t.Fatal(err)
	}

	h.File("evidence/photo.json", `{"claim":"CLM-1","damage":{"kind":"crushed","severity":3}}`)
	h.OK("the evidence/photo.json file is uploaded to the claim-evidence s3 bucket as claims/CLM-1/photo.json")
	h.OK("the evidence/photo.json file is uploaded to the claim-evidence s3 bucket")
	h.OK("the claim-evidence s3 bucket has an object named claims/CLM-1/photo.json")
	h.OK("the claim-evidence s3 bucket has an object named photo.json")
	h.OK("the claims/CLM-1/photo.json object in the claim-evidence s3 bucket is identical to the evidence/photo.json file")
	h.OK("the claims/CLM-1/photo.json object in the claim-evidence s3 bucket has the following properties:", [][]string{
		{"claim", "CLM-1"}, {"$.damage.severity", "3"}, {"damage.note", "undefined"},
	})

	// An object written later is waited for.
	go func() {
		time.Sleep(time.Second)
		_, _ = client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String("claim-evidence"), Key: aws.String("letters/CLM-1.json"), Body: strings.NewReader(`{"decision":"APPROVED"}`),
		})
	}()
	h.OK("within 5s the claim-evidence s3 bucket has an object named letters/CLM-1.json")

	err = h.Fails("within 1s the claim-evidence s3 bucket has an object named letters/CLM-2.json", "has no object named letters/CLM-2.json")
	if !strings.Contains(err.Error(), "claims/CLM-1/photo.json") {
		t.Errorf("the failure should list the bucket: %v", err)
	}
	h.File("expected/other.json", `{}`)
	h.Fails("within 1s the photo.json object in the claim-evidence s3 bucket is identical to the expected/other.json file", "content differs")
	h.Fails("within 1s the photo.json object in the claim-evidence s3 bucket has the following properties:", "claim", [][]string{{"claim", "CLM-9"}})
}
