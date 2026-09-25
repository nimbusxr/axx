// Package awss3 is the aws-s3 pack: files uploaded to S3 buckets and the
// objects the services under test write there.
package awss3

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

const packDoc = `Upload files to S3 buckets and check the objects your services write there.

The steps use the scenario's AWS account (` + "`the {word} aws account with the following properties:`" + `, from aws-core). Checks wait for the object (10 seconds unless ` + "`within {duration}`" + ` says otherwise), since services write asynchronously: an upload that triggers processing (an S3 event notification to a queue, say) and the object that processing writes.`

// Pack returns the aws-s3 pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "aws-s3",
		Namespace: "aws-s3",
		Doc:       packDoc,
		Requires:  []string{awscore.Name},
		Steps: cloudstep.Objects{
			Pack: "aws-s3", Container: "s3 bucket", Object: "object", Example: "carrier-drops",
			Store: store,
		}.Steps(),
	}
}

func store(sc *core.Scenario) (cloudstep.ObjectStore, error) {
	acct, err := awscore.Default(sc)
	if err != nil {
		return nil, err
	}
	cfg, err := acct.Config(sc.Context(), sc.Suite())
	if err != nil {
		return nil, err
	}
	// Emulators serve buckets by path, not by host name.
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = acct.Endpoint != "" })
	return bucketStore{client}, nil
}

type bucketStore struct{ c *s3.Client }

func (b bucketStore) Put(ctx context.Context, bucket, key string, body []byte, contentType string) error {
	_, err := b.c.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentType: aws.String(contentType),
	})
	return err
}

func (b bucketStore) Get(ctx context.Context, bucket, key string) ([]byte, bool, error) {
	out, err := b.c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var nk *types.NoSuchKey
		if errors.As(err, &nk) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	return body, true, err
}

func (b bucketStore) List(ctx context.Context, bucket string, max int) ([]string, error) {
	out, err := b.c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), MaxKeys: aws.Int32(int32(max))})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Contents))
	for _, o := range out.Contents {
		names = append(names, aws.ToString(o.Key))
	}
	return names, nil
}
