package store

import "fmt"

type NotFoundError struct {
	Resource string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found", e.Resource)
}

type AlreadyExistsError struct {
	Resource string
}

func (e *AlreadyExistsError) Error() string {
	return fmt.Sprintf("%s already exists", e.Resource)
}

type InvalidPageTokenError struct {
	Err error
}

func (e *InvalidPageTokenError) Error() string {
	return fmt.Sprintf("invalid page token: %v", e.Err)
}

func (e *InvalidPageTokenError) Unwrap() error {
	return e.Err
}

func NotFound(resource string) error {
	return &NotFoundError{Resource: resource}
}

func AlreadyExists(resource string) error {
	return &AlreadyExistsError{Resource: resource}
}

func InvalidPageToken(err error) error {
	return &InvalidPageTokenError{Err: err}
}
