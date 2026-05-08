package marketplace

import "errors"

// ErrSubmissionNotFound is returned when a submission ID cannot be
// resolved by the SubmissionRepository.
var ErrSubmissionNotFound = errors.New("marketplace submission not found")

// ErrSubmissionAlreadyExists is returned when submit is called twice
// with the same submission ID.
var ErrSubmissionAlreadyExists = errors.New("marketplace submission already exists")

// ErrSubmissionInvalid is returned when the inbound submission row
// fails structural validation: missing tenant, invalid email,
// invalid manifest.
var ErrSubmissionInvalid = errors.New("invalid marketplace submission")
