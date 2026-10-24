// Package install decides how a package gets onto the machine (Plan) and
// carries the decision out.
package install

import (
	"io"

	"github.com/shimpe/scdeken/internal/doctor"
	"github.com/shimpe/scdeken/internal/fetch"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/scenv"
	"github.com/shimpe/scdeken/internal/state"
)

// Service installs packages from an index into one SuperCollider session.
type Service struct {
	Index    *index.Index
	Ctx      scenv.Context
	Paths    scenv.Paths
	State    *state.State
	Fetch    *fetch.Client
	Runner   run.Runner
	Env      doctor.Env
	Log      io.Writer
	Progress func(fraction float32, label string) // may be nil
}
