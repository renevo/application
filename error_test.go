package application

import (
	"errors"
	"testing"

	"github.com/matryer/is"
)

func TestErrorAppend(t *testing.T) {
	is := is.New(t)

	var err *Error
	err1 := errors.New("test 1")
	err2 := errors.New("test 2")

	err = err.Append(err1)
	err = err.Append(err2)

	is.Equal(len(err.Errors), 2)  // the error list should contain both appended errors
	is.Equal(err.Errors[0], err1) // the first appended error should be preserved in order
	is.Equal(err.Errors[1], err2) // the second appended error should be preserved in order
}
