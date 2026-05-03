package loop_test

import (
	"github.com/fgmacedo/buchecha/internal/config"
)

// newTestConfig returns a Config with sensible defaults for loop and
// director-loop integration tests. The driver caps iterations at 50 so
// runs stay bounded; tests that want a tighter cap pass their own value
// downstream.
func newTestConfig() *config.Config {
	return &config.Config{
		Loop: config.Loop{
			MaxIterations: 50,
		},
	}
}
