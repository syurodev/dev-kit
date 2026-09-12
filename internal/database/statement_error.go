package database

import "errors"

/*
An error the engine returned while running a statement, marked as such.

The bridge redacts every database error it does not recognise, and that is the
right default: connecting happens with a DSN in hand, so a dial or an
authentication failure can carry a host, a username — and, if a driver ever
echoes what it was given, a password. None of that belongs in a public error.

A statement's error is the opposite case. It is the engine answering the SQL
the user just typed, it is the single most useful thing the console can show,
and it contains nothing the user did not already write. Marking it is what
lets the bridge tell the two apart instead of treating "relation does not
exist" as a connection that fell over.

Marked at the point the driver returns it, never inferred later by reading the
text: a rule that guesses from the message is a rule that leaks the first time
a driver phrases something the way a connection error is phrased.
*/
type StatementError struct {
	cause error
}

func NewStatementError(cause error) error {
	if cause == nil {
		return nil
	}
	// Already marked — wrapping twice would only add a layer for errors.As to
	// walk, and the inner mark is the same claim.
	var existing *StatementError
	if errors.As(cause, &existing) {
		return cause
	}
	return &StatementError{cause: cause}
}

// Error is the engine's own text, unchanged. The mark is carried by the type,
// not by a prefix: a console shows this string, and "statement error: " in
// front of every SQLSTATE would be noise the reader has to skip.
func (e *StatementError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *StatementError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
