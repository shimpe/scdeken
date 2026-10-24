package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sc-deken/sc-deken/internal/catalog"
	"github.com/sc-deken/sc-deken/internal/config"
	"github.com/sc-deken/sc-deken/internal/doctor"
	"github.com/sc-deken/sc-deken/internal/fetch"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/install"
	"github.com/sc-deken/sc-deken/internal/osc"
	"github.com/sc-deken/sc-deken/internal/scenv"
	"github.com/sc-deken/sc-deken/internal/state"
)

// session is everything one command needs: where log lines and progress go,
// and lazy access to the package index and the install service.
type session struct {
	req      Request
	deps     Deps
	log      io.Writer
	sender   *osc.Sender
	logOSC   io.Closer
	progress func(fraction float32, label string)
}

// newSession sets up logging to stderr and, when SuperCollider listens on an
// OSC port, to SuperCollider too.
func newSession(req Request, deps Deps) *session {
	s := &session{req: req, deps: deps, log: deps.Stderr}
	if req.Context.OSCPort <= 0 {
		return s
	}
	sender, err := osc.Dial(req.Context.OSCPort)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "Cannot reach SuperCollider on port %d: %v\n", req.Context.OSCPort, err)
		return s
	}
	job := req.Context.Job
	w := osc.LogWriter(sender, job)
	s.sender, s.logOSC = sender, w
	s.log = io.MultiWriter(deps.Stderr, w)
	s.progress = func(fraction float32, label string) { sender.Progress(job, fraction, label) }
	return s
}

// close flushes the last log line and releases the OSC connection.
func (s *session) close() {
	if s.logOSC != nil {
		s.logOSC.Close()
	}
	if s.sender != nil {
		s.sender.Close()
	}
}

// paths returns sc-deken's folders for this session.
func (s *session) paths() (scenv.Paths, error) {
	if s.req.Context.AppSupportDir == "" {
		return scenv.Paths{}, errors.New("SuperCollider did not say where its settings folder is, so there is nowhere to keep sc-deken's files")
	}
	return s.req.Context.Paths(), nil
}

// loadIndex returns the signed package index, asking the server again when
// maxAge is not zero and the saved copy is older.
func (s *session) loadIndex(ctx context.Context, maxAge time.Duration) (*index.Index, error) {
	paths, err := s.paths()
	if err != nil {
		return nil, err
	}
	url := s.req.IndexURL
	if url == "" {
		url = config.DefaultIndexURL
	}
	return catalog.Load(ctx, fetch.New(paths.Cache), catalog.Options{
		URL: url, Offline: s.req.Offline, MaxAge: maxAge,
		TrustedKeysPath: paths.TrustedKeys, EmbeddedKey: s.deps.EmbeddedKey,
		Dir: paths.Catalog, Log: s.log,
	})
}

// service returns the install service without a package index. Use
// serviceWithIndex for commands that look packages up.
func (s *session) service() (*install.Service, error) {
	paths, err := s.paths()
	if err != nil {
		return nil, err
	}
	c := s.req.Context
	st, err := state.Load(paths.State)
	if err != nil {
		return nil, err
	}
	return &install.Service{
		Ctx: c, Paths: paths, State: st, Fetch: fetch.New(paths.Cache),
		Runner: s.deps.Runner, Env: doctor.DetectEnv(), Log: s.log, Progress: s.progress,
	}, nil
}

// serviceWithIndex is service plus the signed package index.
func (s *session) serviceWithIndex(ctx context.Context) (*install.Service, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if svc.Index, err = s.loadIndex(ctx, 0); err != nil {
		return nil, err
	}
	return svc, nil
}
