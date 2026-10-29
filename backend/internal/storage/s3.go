package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type s3Adapter struct {
	client   *s3.Client
	transfer *transfermanager.Client
	bucket   string
	prefix   string
	expires  time.Duration
}

type s3ProgressListener struct{ report func(int64, int64) }

func (p s3ProgressListener) OnObjectBytesTransferred(_ context.Context, event *transfermanager.ObjectBytesTransferredEvent) {
	if p.report != nil {
		p.report(event.BytesTransferred, event.TotalBytes)
	}
}

// NewS3 supports AWS S3 and compatible services with explicit credentials.
func NewS3(cfg Config, client *http.Client) (Adapter, error) {
	bucket := strings.TrimSpace(cfg.Settings["bucket"])
	region := strings.TrimSpace(cfg.Settings["region"])
	if bucket == "" || strings.ContainsAny(bucket, "/\\ ") || region == "" {
		return nil, newCloudError(ClassConfig, "s3_bucket_region_required", false)
	}
	if cfg.Secrets["access_key_id"] == "" || cfg.Secrets["secret_access_key"] == "" {
		return nil, newCloudError(ClassConfig, "s3_credentials_required", false)
	}
	parts, cerr := validatePathPrefix(cfg.Settings["path_prefix"])
	if cerr != nil {
		return nil, cerr
	}
	pathStyle := false
	if v := cfg.Settings["path_style"]; v != "" {
		var err error
		pathStyle, err = strconv.ParseBool(v)
		if err != nil {
			return nil, newCloudError(ClassConfig, "s3_path_style_invalid", false)
		}
	}
	expires := 3600
	if v := cfg.Settings["share_expires_seconds"]; v != "" {
		var err error
		expires, err = strconv.Atoi(v)
		if err != nil || expires < 60 || expires > 604800 {
			return nil, newCloudError(ClassConfig, "s3_share_expiry_invalid", false)
		}
	}
	endpoint := strings.TrimSpace(cfg.Settings["endpoint"])
	if endpoint != "" {
		if _, err := normalizeHTTPBase(endpoint); err != nil {
			return nil, newCloudError(ClassConfig, "s3_endpoint_invalid", false)
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	base := aws.Config{Region: region, Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(cfg.Secrets["access_key_id"], cfg.Secrets["secret_access_key"], cfg.Secrets["session_token"])), HTTPClient: client,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired, ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired}
	s3client := s3.NewFromConfig(base, func(o *s3.Options) {
		o.UsePathStyle = pathStyle
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	uploader := transfermanager.New(s3client, func(o *transfermanager.Options) {
		o.Concurrency = 3
		o.PartSizeBytes = 8 << 20
		o.FailTimeout = 15 * time.Second
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	})
	return &s3Adapter{client: s3client, transfer: uploader, bucket: bucket, prefix: strings.Join(parts, "/"), expires: time.Duration(expires) * time.Second}, nil
}

func (a *s3Adapter) key(key string) (string, error) {
	if _, err := validateObjectKey(key); err != nil {
		return "", err
	}
	if a.prefix == "" {
		return key, nil
	}
	return a.prefix + "/" + key, nil
}

func (a *s3Adapter) Put(ctx context.Context, obj Object) (Stored, error) {
	key, err := a.key(obj.Key)
	if err != nil {
		return Stored{}, err
	}
	if obj.Size < 0 {
		return Stored{}, newCloudError(ClassInvalidInput, "object_size_negative", false)
	}
	if obj.SHA256 != "" {
		if _, ok := normalizeSHA256(obj.SHA256); !ok {
			return Stored{}, newCloudError(ClassInvalidInput, "sha256_invalid", false)
		}
	}
	f, err := os.Open(obj.Path)
	if err != nil {
		return Stored{}, newCloudError(ClassFileMissing, "s3_source_missing", false)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Stored{}, newCloudError(ClassInvalidInput, "s3_source_invalid", false)
	}
	if info.Size() != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "s3_source_size_mismatch", false)
	}
	_, err = a.transfer.UploadObject(ctx, &transfermanager.UploadObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key), Body: f, ContentLength: aws.Int64(obj.Size), ContentType: aws.String("application/octet-stream")}, func(o *transfermanager.Options) {
		o.ObjectProgressListeners.Register(s3ProgressListener{report: obj.Progress})
	})
	if err != nil {
		return Stored{}, s3Error(ctx, err, "s3_put")
	}
	head, err := a.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key)})
	if err != nil {
		return Stored{}, s3Error(ctx, err, "s3_verify")
	}
	if head.ContentLength == nil || *head.ContentLength != obj.Size {
		return Stored{}, newCloudError(ClassSizeMismatch, "s3_size_mismatch", false)
	}
	return Stored{RemoteID: obj.Key, Size: obj.Size, Verification: "size"}, nil
}

func (a *s3Adapter) Open(ctx context.Context, remote string) (io.ReadCloser, error) {
	key, err := a.key(remote)
	if err != nil {
		return nil, err
	}
	out, err := a.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, s3Error(ctx, err, "s3_open")
	}
	return out.Body, nil
}

func (a *s3Adapter) Share(ctx context.Context, remote string) (Share, error) {
	key, err := a.key(remote)
	if err != nil {
		return Share{}, err
	}
	request, err := s3.NewPresignClient(a.client).PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(key)}, func(o *s3.PresignOptions) { o.Expires = a.expires })
	if err != nil {
		return Share{}, s3Error(ctx, err, "s3_share")
	}
	expires := time.Now().Add(a.expires)
	return Share{URL: request.URL, ExpiresAt: &expires, Status: "ready"}, nil
}
func (a *s3Adapter) Health(ctx context.Context) error {
	_, err := a.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(a.bucket)})
	if err != nil {
		return s3Error(ctx, err, "s3_health")
	}
	return nil
}
func (a *s3Adapter) Capabilities() Capabilities {
	return Capabilities{Upload: true, Download: true, Share: true}
}

func s3Error(ctx context.Context, err error, op string) error {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) {
		return statusError(op, response.HTTPStatusCode())
	}
	var api smithy.APIError
	if errors.As(err, &api) && (api.ErrorCode() == "AccessDenied" || api.ErrorCode() == "InvalidAccessKeyId") {
		return newCloudError(ClassAuth, op+"_auth", false)
	}
	return transportError(ctx, err, op)
}
