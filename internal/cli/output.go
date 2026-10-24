package cli

import (
	"encoding/json"
	"errors"

	"github.com/shimpe/scdeken/internal/install"
)

// Exit codes of scdeken.
const (
	exitOK      = 0
	exitError   = 1
	exitUsage   = 2
	exitConfirm = 3 // the user must confirm something first
)

// usageError marks a command that was typed or requested wrongly.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// exitCode maps the outcome of a command to the process exit code.
func exitCode(err error) int {
	var usage usageError
	var confirm *install.ConfirmationError
	switch {
	case err == nil:
		return exitOK
	case errors.As(err, &usage):
		return exitUsage
	case errors.As(err, &confirm):
		return exitConfirm
	default:
		return exitError
	}
}

// newResponse turns a command's outcome into the Response to send back.
func newResponse(data any, err error) Response {
	if err == nil {
		return Response{OK: true, Data: data}
	}
	resp := Response{Error: err.Error(), Data: data}
	var confirm *install.ConfirmationError
	if errors.As(err, &confirm) {
		resp.Confirm = confirm.Needed
	}
	return resp
}

// encodeResponse renders a Response as indented JSON ending in a newline.
func encodeResponse(resp Response) []byte {
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		data, _ = json.Marshal(Response{Error: "cannot encode the answer: " + err.Error()})
	}
	return append(data, '\n')
}
