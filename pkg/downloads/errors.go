package downloads

import "errors"

var (
	ErrInvalidURL        = errors.New("invalid download URL")
	ErrPathOutsideRoot   = errors.New("target directory is outside the download root")
	ErrInvalidPath       = errors.New("invalid target directory")
	ErrTaskNotFound      = errors.New("download task not found")
	ErrTaskConflict      = errors.New("download task conflicts with an existing task or file")
	ErrInvalidTransition = errors.New("download task cannot transition from its current state")
)
