package expr

import (
	"fmt"
	"strings"
)

type Error struct {
	Message string
	Pos     Position
	Input   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s at line %d, column %d", e.Message, e.Pos.Line, e.Pos.Column)
}

func (e *Error) Annotated() string {
	lines := strings.Split(e.Input, "\n")
	index := e.Pos.Line - 1
	if index < 0 || index >= len(lines) {
		return e.Error()
	}

	line := lines[index]
	caret := e.Pos.Column - 1
	if caret < 0 {
		caret = 0
	}
	if caret > len(line) {
		caret = len(line)
	}

	var builder strings.Builder
	builder.WriteString(e.Error())
	builder.WriteString("\n  ")
	builder.WriteString(line)
	builder.WriteString("\n  ")
	builder.WriteString(strings.Repeat(" ", caret))
	builder.WriteString("^")
	return builder.String()
}

func newError(input, message string, pos Position) *Error {
	return &Error{Message: message, Pos: pos, Input: input}
}
