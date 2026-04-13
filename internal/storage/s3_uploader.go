package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog"
)

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	PathStyle bool
}

type S3Uploader struct {
	client *s3.Client
	bucket string
	logger zerolog.Logger
}

func NewS3Uploader(ctx context.Context, cfg S3Config, logger zerolog.Logger) (*S3Uploader, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("storage: s3 bucket must be configured")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	var opts []func(*config.LoadOptions) error
	opts = append(opts, config.WithRegion(region))
	if cfg.AccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")))
	}
	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}
	cli := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
	})
	return &S3Uploader{
		client: cli,
		bucket: cfg.Bucket,
		logger: logger.With().Str("component", "s3_uploader").Logger(),
	}, nil
}

func (u *S3Uploader) Upload(ctx context.Context, bucket, key string, data []byte) error {
	if bucket == "" {
		bucket = u.bucket
	}
	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		u.logger.Error().Err(err).Str("bucket", bucket).Str("key", key).Msg("s3 upload failed")
		return fmt.Errorf("storage: put object: %w", err)
	}
	u.logger.Debug().Str("bucket", bucket).Str("key", key).Int("bytes", len(data)).Msg("s3 upload ok")
	return nil
}

func (u *S3Uploader) HealthCheck(ctx context.Context) error {
	_, err := u.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(u.bucket)})
	return err
}

func (u *S3Uploader) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	if bucket == "" {
		bucket = u.bucket
	}
	out, err := u.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: get object: %w", err)
	}
	defer out.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(out.Body); err != nil {
		return nil, fmt.Errorf("storage: read object body: %w", err)
	}
	u.logger.Debug().Str("bucket", bucket).Str("key", key).Int("bytes", buf.Len()).Msg("s3 download ok")
	return buf.Bytes(), nil
}

func (u *S3Uploader) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	if bucket == "" {
		bucket = u.bucket
	}
	presignClient := s3.NewPresignClient(u.client)
	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("storage: presign get object: %w", err)
	}
	return req.URL, nil
}

func (u *S3Uploader) Delete(ctx context.Context, bucket, key string) error {
	if bucket == "" {
		bucket = u.bucket
	}
	_, err := u.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		u.logger.Error().Err(err).Str("bucket", bucket).Str("key", key).Msg("s3 delete failed")
		return fmt.Errorf("storage: delete object: %w", err)
	}
	u.logger.Debug().Str("bucket", bucket).Str("key", key).Msg("s3 delete ok")
	return nil
}
