package grominio

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
)

func TestGrominio(t *testing.T) {
	suite.Case(t)
}

func TestBoostrap(t *testing.T) {
	type TestDeps struct{}

	t.Run("should be able fail at running container", func(t *testing.T) {
		exp := errors.New(uuid.NewString())

		cfg := config{
			runner: func(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (MinioContainer, error) {
				return nil, exp
			},
		}

		res, err := bootstrapper[TestDeps](cfg)(t.Context())
		require.ErrorIs(t, err, exp)
		require.Nil(t, res)
	})

	t.Run("should be able fail at take container connection string", func(t *testing.T) {
		exp := errors.New(uuid.NewString())
		con := NewMockMinioContainer(t)

		con.EXPECT().ConnectionString(mock.Anything).Return("", exp)

		cfg := config{
			runner: func(
				ctx context.Context,
				img string,
				opts ...testcontainers.ContainerCustomizer,
			) (MinioContainer, error) {
				return con, nil
			},
			terminator: func(ctx context.Context, terminate func(context.Context, ...testcontainers.TerminateOption) error) func() {
				return func() {}
			},
		}

		res, err := bootstrapper[TestDeps](cfg)(t.Context())

		require.ErrorIs(t, err, exp)
		require.Nil(t, res)
	})
}

func TestWrapper(t *testing.T) {
	w := wrapMinioContainer{}
	assert.Panics(t, func() {
		_ = w.Terminate(t.Context())
	})
}

func TestMinioContainerRunner(t *testing.T) {
	res, err := minioContainerRunner(t.Context(), "minio:minio", minio.WithUsername(""))
	require.Error(t, err)
	assert.Nil(t, res)
}
