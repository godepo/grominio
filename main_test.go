package grominio

import (
	"os"
	"testing"

	"github.com/godepo/groat"
	"github.com/godepo/groat/integration"
	"github.com/godepo/grominio/internal/pkg/containersync"
)

type SystemUnderTest struct {
}

type State struct {
}

type Deps struct {
}

var suite *integration.Container[Deps, State, *SystemUnderTest]

func TestMain(m *testing.M) {
	suite = integration.New[Deps, State, *SystemUnderTest](m,
		func(t *testing.T) *groat.Case[Deps, State, *SystemUnderTest] {
			tcs := groat.New[Deps, State, *SystemUnderTest](t, func(t *testing.T, deps Deps) *SystemUnderTest {
				return &SystemUnderTest{}
			})
			return tcs
		},
		New[Deps](
			WithTerminator(containersync.Terminator),
			WithBucketPrefix("test-"),
		),
	)
	os.Exit(suite.Go())
}
