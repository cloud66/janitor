package executors

import (
	"io"
	"os"
)

// out_sink returns the writer all executor warning/log output should flow
// through. tests swap this by setting testOut; production returns stdout.
// TODO: replace with core.Warnf once Phase 4 introduces a shared helper.
var testOut io.Writer

// out_sink returns the active output writer.
func out_sink() io.Writer {
	if testOut != nil {
		return testOut
	}
	return os.Stdout
}
