// Package install decides how a package gets onto the machine (Plan) and
// carries the decision out.
package install

import (
	"io"

	"github.com/sc-deken/sc-deken/internal/doctor"
	"github.com/sc-deken/sc-deken/internal/fetch"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/run"
	"github.com/sc-deken/sc-deken/internal/scenv"
	"github.com/sc-deken/sc-deken/internal/state"
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
