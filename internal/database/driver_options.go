package database

import (
	"errors"
	"fmt"
)

// ErrDriverOptionNotAllowed is a driver option outside the engine's allowlist.
var ErrDriverOptionNotAllowed = errors.New("driver option is not allowed")

/*
The driver options a profile may carry, per engine (#45).

No part of the UI writes these, but a profile's metadata is synced, and an
option is a DSN parameter: `sslmode=disable`, `allowAllFiles=true`,
`multiStatements=true` and `transaction_read_only=0` are all options too. An
allowlist of the ones that tune a session rather than weaken one, and nothing
else — a new need adds its key here.
*/
var (
	postgresOptions = map[string]bool{
		"application_name":                    true,
		"connect_timeout":                     true,
		"search_path":                         true,
		"statement_timeout":                   true,
		"lock_timeout":                        true,
		"idle_in_transaction_session_timeout": true,
		"timezone":                            true,
	}
	mysqlOptions = map[string]bool{
		"charset":          true,
		"collation":        true,
		"loc":              true,
		"timeout":          true,
		"readTimeout":      true,
		"writeTimeout":     true,
		"maxAllowedPacket": true,
	}
)

func checkDriverOptions(allowed map[string]bool, options map[string]string) error {
	for key := range options {
		if !allowed[key] {
			return fmt.Errorf("%w: %q", ErrDriverOptionNotAllowed, key)
		}
	}
	return nil
}
