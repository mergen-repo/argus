//go:build integration

package storage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog"
)

func TestS3Upload_Integration(t *testing.T) {
	endpoint := os.Getenv("S3_INTEGRATION_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_INTEGRATION_ENDPOINT not set")
	}

	ctx := context.Background()
	logger := zerolog.Nop()

	cfg := S3Config{
		Endpoint:  endpoint,
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Bucket:    "test-bucket",
		Region:    "us-east-1",
		PathStyle: true,
	}

	u, err := NewS3Uploader(ctx, cfg, logger)
	if err != nil {
		t.Fatalf("NewS3Uploader: %v", err)
	}

	_, createErr := u.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if createErr != nil {
		t.Logf("CreateBucket (may already exist): %v", createErr)
	}

	key := fmt.Sprintf("test/%s.txt", time.Now().Format(time.RFC3339))
	data := []byte("hello")

	if err := u.Upload(ctx, "", key, data); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	head, err := u.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(cfg.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("HeadObject: %v", err)
	}

	if head.ContentLength == nil || *head.ContentLength != 5 {
		var cl int64
		if head.ContentLength != nil {
			cl = *head.ContentLength
		}
		t.Errorf("ContentLength = %d, want 5", cl)
	}
}
