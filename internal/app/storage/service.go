package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	UseSSL          bool
}

type Service struct {
	client *minio.Client
}

func NewService(cfg Config) (*Service, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}
	return &Service{client: client}, nil
}

type BucketInfo struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Region  string    `json:"region,omitempty"`
}

func (s *Service) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	buckets, err := s.client.ListBuckets(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]BucketInfo, len(buckets))
	for i, b := range buckets {
		result[i] = BucketInfo{
			Name:    b.Name,
			Created: b.CreationDate,
		}
	}
	return result, nil
}

func (s *Service) CreateBucket(ctx context.Context, name string) error {
	return s.client.MakeBucket(ctx, name, minio.MakeBucketOptions{})
}

func (s *Service) DeleteBucket(ctx context.Context, name string) error {
	return s.client.RemoveBucket(ctx, name)
}

type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ContentType  string    `json:"content_type,omitempty"`
	IsDir        bool      `json:"is_dir"`
}

func (s *Service) ListObjects(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	objects := make([]ObjectInfo, 0)

	objectCh := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: false,
	})

	for obj := range objectCh {
		if obj.Err != nil {
			return nil, obj.Err
		}

		isDir := len(obj.Key) > 0 && obj.Key[len(obj.Key)-1] == '/'
		objects = append(objects, ObjectInfo{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
			ContentType:  obj.ContentType,
			IsDir:        isDir,
		})
	}

	return objects, nil
}

func (s *Service) UploadObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

func (s *Service) DownloadObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
}

func (s *Service) DeleteObject(ctx context.Context, bucket, key string) error {
	return s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}

func (s *Service) GetPresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, bucket, key, expiry, nil)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}
