//go:generate go tool mockery
package grominio

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
		hostedDSN      string
		bucketPrefix   string
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
		forks        *atomic.Int32
		userName     string
		password     string
		connString   string
		ctx          context.Context
		cfg          config
		bucketPrefix string
	}
)
type Option func(*config)

func WithTerminator(terminator Terminator) Option {
	return func(o *config) {
		o.terminator = terminator
	}
}

func WithBucketPrefix(prefix string) Option {
	return func(c *config) {
		c.bucketPrefix = prefix
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
	cfg config,
	connString string,
	userName string,
	password string,
) *Container[T] {
	container := &Container[T]{
		forks:        &atomic.Int32{},
		userName:     userName,
		password:     password,
		ctx:          ctx,
		cfg:          cfg,
		bucketPrefix: cfg.bucketPrefix,
	}

	container.connString = connString

	return container
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

	if env := os.Getenv("GROAT_I9N_MINIO_DSN"); env != "" {
		cfg.hostedDSN = env
	}

	return bootstrapper[T](cfg)
}

func (c *Container[T]) Injector(t *testing.T, to T) T {
	t.Helper()

	bucket := fmt.Sprintf("%s%d", c.bucketPrefix, c.forks.Add(1))
	s3Config := aws.NewConfig()

	s3Config.Credentials = credentials.NewStaticCredentialsProvider(
		c.userName,
		c.password,
		"")

	s3Config.BaseEndpoint = aws.String("http://" + c.connString)
	s3Config.Region = "us-east-1"

	s3Client := s3.NewFromConfig(*s3Config, func(options *s3.Options) {
		options.UsePathStyle = true
	})

	_, err := s3Client.CreateBucket(t.Context(), &s3.CreateBucketInput{
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
		if cfg.hostedDSN == "" {
			mc, err := cfg.runner(ctx, cfg.containerImage)
			if err != nil {
				return nil, fmt.Errorf("error creating minio container: %w", err)
			}

			ctxgroup.IncAt(ctx)

			go cfg.terminator(ctx, mc.Terminate)()

			connString, err := mc.ConnectionString(ctx)
			if err != nil {
				return nil, err
			}

			container := newContainer[T](ctx, cfg, connString, mc.UserName(), mc.Password())

			return container.Injector, nil
		}

		dsn, err := url.Parse(cfg.hostedDSN)
		if err != nil {
			return nil, fmt.Errorf("error parsing hosted DSN: %w", err)
		}

		var userName, password string
		if dsn.User != nil {
			userName = dsn.User.Username()
			password, _ = dsn.User.Password()
		}

		container := newContainer[T](ctx, cfg, dsn.Hostname()+":"+dsn.Port(), userName, password)

		return container.Injector, nil
	}
}
