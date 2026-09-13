package bridge

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/database"
)

/*
What the console is allowed to say when a statement fails.

The bridge redacts by default, and for most subjects that is right: never let
an upstream response, an endpoint or a bearer token out through the public
error contract. But a SQL console's whole product is the engine's own
sentence. "relation \"user\" does not exist / HINT: Perhaps you meant
\"users\"" is the most useful thing the app can put on screen, and turning it
into "Connection failed." is not redaction — it is a wrong answer, because the
connection is fine.

So exactly one thing passes through: an error the engine returned while
running a statement. Everything else on this subject stays redacted, because
connecting is where a DSN — and therefore a password — can end up inside an
error string.
*/

func TestStatementErrorReachesTheConsole(t *testing.T) {
	engine := errors.New(`ERROR: relation "user" does not exist (SQLSTATE 42P01)`)
	err := publicError("builtin.database", database.NewStatementError(engine))

	if err == nil {
		t.Fatal("publicError returned nil")
	}
	if !strings.Contains(err.Message, `relation "user" does not exist`) {
		t.Fatalf("the engine's own sentence did not survive: %q", err.Message)
	}
	if err.Message == "Connection failed." {
		t.Fatal("a failed statement was reported as a failed connection")
	}
}

// The safe-mode banner and the SQLSTATE chip in the console both read the
// message for a code. They were unreachable for as long as every message was
// the same four words.
func TestReadOnlyRejectionKeepsItsSQLSTATE(t *testing.T) {
	engine := errors.New("ERROR: cannot execute UPDATE in a read-only transaction (SQLSTATE 25006)")
	err := publicError("builtin.database", database.NewStatementError(engine))

	if err == nil || !strings.Contains(err.Message, "25006") {
		t.Fatalf("SQLSTATE did not survive: %v", err)
	}
}

// Anything that is not a statement result still gives nothing away: dialling,
// authenticating and resolving a profile all happen with a DSN in hand.
func TestConnectionTroubleStaysRedacted(t *testing.T) {
	err := publicError("builtin.database", errors.New("dial tcp 10.0.0.4:5432: password=hunter2"))

	if err == nil || err.Message != "Connection failed." {
		t.Fatalf("public error = %v", err)
	}
}

/*
A console is a file, and a file failing is not a connection failing.

The catch-all below these cases answers "Connection failed." for everything
on this subject, which sent the reader to the database they were about to
blame for a full disk. The path itself stays out of the message — it names
the user's home directory.
*/
func TestConsoleStorageFailureIsNotReportedAsAConnection(t *testing.T) {
	err := publicError("builtin.database", fmt.Errorf("%w: open /Users/someone/DevKit/consoles/c1.sql: permission denied", database.ErrConsoleStore))

	if err == nil {
		t.Fatal("publicError returned nil")
	}
	if err.Message == "Connection failed." {
		t.Fatal("a file that could not be written was reported as a failed connection")
	}
	if strings.Contains(err.Message, "/Users/") {
		t.Fatalf("the message carries a path: %q", err.Message)
	}
}

func TestConsolesWithNowhereToGoSayThat(t *testing.T) {
	err := publicError("builtin.database", database.ErrConsoleStoreUnavailable)
	if err == nil || err.Message == "Connection failed." {
		t.Fatalf("public error = %v", err)
	}
}
