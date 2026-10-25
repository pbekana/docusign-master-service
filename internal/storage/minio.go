package storage

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/your-org/docusign-master-service/internal/config"
)

// MinIOStorage uploads output objects to a MinIO (S3-compatible) bucket.
type MinIOStorage struct {
	client *minio.Client
	bucket string
}

// NewMinIOStorage initialises a MinIO client from cfg and returns a MinIOStorage.
// It does NOT ensure the bucket exists — call EnsureBucket before uploading.
func NewMinIOStorage(cfg *config.Config) (*MinIOStorage, error) {
	client, err := minio.New(cfg.MinioEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinioAccessKey, cfg.MinioSecretKey, ""),
		Secure: cfg.MinioUseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio storage: initialising client: %w", err)
	}
	return &MinIOStorage{
		client: client,
		bucket: cfg.MinioBucketName,
	}, nil
}

// EnsureBucket creates the configured bucket if it does not already exist.
// It calls BucketExists first to avoid the BucketAlreadyOwnedByYou error.
func (s *MinIOStorage) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("minio storage: checking bucket existence: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("minio storage: creating bucket %s: %w", s.bucket, err)
	}
	return nil
}

// UploadObject uploads data to objectKey in the configured bucket.
func (s *MinIOStorage) UploadObject(ctx context.Context, objectKey string, data []byte, contentType string) error {
	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, s.bucket, objectKey, reader, int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType},
	)
	if err != nil {
		return fmt.Errorf("minio storage: uploading %s: %w", objectKey, err)
	}
	return nil
}

// ObjectKey returns the canonical object key for a data type, org, and processing date.
// Format: docusign/{dataType}/organization_id={orgID}/processing_date={date}/{dataType}.parquet
func (s *MinIOStorage) ObjectKey(orgID, dataType string, processingDate time.Time) string {
	date := processingDate.UTC().Format("2006-01-02")
	return fmt.Sprintf("docusign/%s/organization_id=%s/processing_date=%s/%s.parquet",
		dataType, orgID, date, dataType)
}
