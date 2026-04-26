package beakid

import "errors"

// Sentinel errors returned by Generator and FromBase62.
var (
	// ErrBlocked is returned by Generate when all 10 virtual time windows are
	// exhausted. The generator unblocks automatically on the next UpdateTime call
	// once real time has advanced far enough.
	ErrBlocked = errors.New("generator is blocked: virtual windows exhausted")

	// ErrInvalidWorkerID is returned by TryNew when workerID >= 1024.
	ErrInvalidWorkerID = errors.New("worker_id must be less than 1024")

	// ErrInvalidBase62 is returned by FromBase62 when the string is not a valid
	// 11-character base62 value.
	ErrInvalidBase62 = errors.New("invalid base62 string")
)
