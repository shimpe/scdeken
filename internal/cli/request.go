package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sc-deken/sc-deken/internal/install"
	"github.com/sc-deken/sc-deken/internal/scenv"
)

// Request is one command for scdeken, either built from command-line flags or
// read from the request file SuperCollider writes.
type Request struct {
	Command           string          `json:"command"`
	Package           string          `json:"package,omitempty"`
	Version           string          `json:"version,omitempty"`
	Query             string          `json:"query,omitempty"`
	Kind              string          `json:"kind,omitempty"`
	IncludeUnreviewed bool            `json:"include_unreviewed,omitempty"`
	Choices           install.Choices `json:"choices"`
	Path              string          `json:"path,omitempty"`
	URL               string          `json:"url,omitempty"`
	Out               string          `json:"out,omitempty"`
	Build             bool            `json:"build,omitempty"`
	SCVersion         string          `json:"sc_version,omitempty"`
	Commit            string          `json:"commit,omitempty"`
	AdvisoryID        string          `json:"advisory_id,omitempty"`
	Offline           bool            `json:"offline,omitempty"`
	IndexURL          string          `json:"index_url,omitempty"`
	Context           scenv.Context   `json:"context"`
}

// Response is what scdeken answers: printed on stdout, or written to the
// result file by "run".
type Response struct {
	OK      bool     `json:"ok"`
	Data    any      `json:"data,omitempty"`
	Error   string   `json:"error,omitempty"`
	Confirm []string `json:"confirm,omitempty"` // with exit code 3
}

// readRequest loads a request file.
func readRequest(path string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, fmt.Errorf("read request %s: %w", path, err)
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return Request{}, fmt.Errorf("parse request %s: %w", path, err)
	}
	return req, nil
}

// writeFileAtomic writes data to a temporary file next to path and renames it
// into place, so a reader sees the old file or the whole new one.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".result-*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := firstError(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
