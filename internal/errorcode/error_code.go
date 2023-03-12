package errorcode

import (
	"github.com/palantir/stacktrace"
)

// List of error codes that can be embedded into errors.
const (
	EcodeNoError = stacktrace.ErrorCode(iota)
	EcodeServiceNotStarted
	EcodeServiceTimeout
	EcodeServiceStopping
	EcodeServiceStopped
)
