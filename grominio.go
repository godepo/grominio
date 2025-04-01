//go:generate go tool mockery
package grominio

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/godepo/groat/pkg/ctxgroup"

	"github.com/godepo/groat/integration"
	"github.com/godepo/groat/pkg/generics"
	"github.com/godepo/grominio/internal/pkg/containersync"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
)

type (
	minioRunner func(
		ctx context.Context,
		img string,
		opts ...testcontainers.ContainerCustomizer,
	) (MinioContainer, error)
	config struct {
		containerImage string
		runner         minioRunner
		terminator     Terminator
	}

	Terminator func(
		ctx context.Context,
		terminate func(context.Context,
			...testcontainers.TerminateOption) error,
	) func()

	MinioContainer interface {
		ConnectionString(ctx context.Context) (string, error)
		Terminate(ctx context.Context, opts ...testcontainers.TerminateOption) error
		UserName() string
		Password() string
	}

	Container[T any] struct {
		forks      *atomic.Int32
		minio      MinioContainer
		connString string
		ctx        context.Context
		cfg        config
	}
)
type Option func(*config)

func WithTerminator(terminator Terminator) Option {
	return func(o *config) {
		o.terminator = terminator
	}
}

type wrapMinioContainer struct {
	container *minio.MinioContainer
}

func (w wrapMinioContainer) ConnectionString(ctx context.Context) (string, error) {
	return w.container.ConnectionString(ctx)
}

func (w wrapMinioContainer) Terminate(ctx context.Context, opts ...testcontainers.TerminateOption) error {
	return w.container.Terminate(ctx, opts...)
}

func (w wrapMinioContainer) UserName() string {
	return w.container.Username
}

func (w wrapMinioContainer) Password() string {
	return w.container.Password
}

func minioContainerRunner(
	ctx context.Context,
	img string,
	opts ...testcontainers.ContainerCustomizer,
) (MinioContainer, error) {
	mc, err := minio.Run(ctx, img, opts...)
	if err != nil {
		return nil, err
	}
	return wrapMinioContainer{container: mc}, nil
}

func newContainer[T any](ctx context.Context,
	mc MinioContainer,
	cfg config,
) (*Container[T], error) {
	container := &Container[T]{
		forks: &atomic.Int32{},
		minio: mc,
		ctx:   ctx,
		cfg:   cfg,
	}

	connString, err := mc.ConnectionString(ctx)
	if err != nil {
		return nil, err
	}
	container.connString = connString

	return container, nil
}

func New[T any](all ...Option) integration.Bootstrap[T] {
	cfg := config{
		containerImage: "minio/minio:RELEASE.2024-01-16T16-07-38Z",
		runner:         minioContainerRunner,
		terminator:     containersync.Terminator,
	}

	for _, opt := range all {
		opt(&cfg)
	}

	return bootstrapper[T](cfg)
}

func (c *Container[T]) Injector(t *testing.T, to T) T {
	t.Helper()

	bucket := fmt.Sprintf("bucket%d", c.forks.Add(1))

	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(c.minio.UserName(), c.minio.Password(), ""),
		Endpoint:         aws.String(c.connString),
		Region:           aws.String("us-east-1"),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}

	sess, err := session.NewSession(s3Config)
	require.NoError(t, err)

	s3Client := s3.New(sess)

	_, err = s3Client.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	require.NoError(t, err)

	to = generics.Injector(t, s3Client, to, "s3client")
	to = generics.Injector(t, bucket, to, "s3bucketname")
	to = generics.Injector(t, c.connString, to, "s3url")
	return to
}

func bootstrapper[T any](cfg config) integration.Bootstrap[T] {
	return func(ctx context.Context) (integration.Injector[T], error) {
		mc, err := cfg.runner(ctx, cfg.containerImage)
		if err != nil {
			return nil, fmt.Errorf("error creating minio container: %w", err)
		}

		ctxgroup.IncAt(ctx)

		go cfg.terminator(ctx, mc.Terminate)()
		container, err := newContainer[T](ctx, mc, cfg)
		if err != nil {
			return nil, fmt.Errorf("error creating minio container: %w", err)
		}

		return container.Injector, nil
	}
}
