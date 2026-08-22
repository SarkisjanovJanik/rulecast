package flag

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrVersionExists = errors.New("version already exists")
)

type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 1 {
		return fmt.Sprintf("%s: %s", e.Problems[0].Path, e.Problems[0].Message)
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "%d validation problems:", len(e.Problems))
	for _, problem := range e.Problems {
		fmt.Fprintf(&builder, "\n  %s: %s", problem.Path, problem.Message)
	}
	return builder.String()
}

func (e *ValidationError) add(path, format string, args ...any) {
	e.Problems = append(e.Problems, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (e *ValidationError) orNil() error {
	if len(e.Problems) == 0 {
		return nil
	}
	return e
}
