# Task 6: Database TLS, Pooling, and Cancellation Report

## Implemented changes

- Added profile TLS normalization: remote profiles default to `verify-full`;
  explicit `sslmode=disable` is rejected unless the host is `localhost` or a
  parsed loopback IP address.
- Added a `PoolManager` that caches one `pgxpool.Pool` per profile ID, with
  `MaxConns=4`, `MinConns=0`, and `MaxConnLifetime=30m`.
- Database connection tests and queries now use the pool with a 30-second
  caller-derived context deadline; query rows are still capped and closed.
- Deleting a profile closes its pool; `Service.Close` closes all pools for
  application shutdown.
- Existing database-profile password encryption remains V2 and bound to
  `database-profile`, the profile ID, and `db-password` key ID.

## TDD evidence

1. RED profile-validation tests:

   `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database -count=1`

   Result: failed as expected because `ErrInsecureTLSForRemoteHost` did not
   yet exist.

2. GREEN profile-validation tests:

   `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database -count=1`

   Result: `ok changeme/internal/services/database 2.077s`.

3. RED pool tests:

   `GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database -count=1`

   Result: failed as expected because `NewPoolManager` and `queryContext` did
   not yet exist.

## Completion verification

`go mod tidy` added the required indirect `pgxpool` dependencies
(`github.com/jackc/puddle/v2` and `golang.org/x/sync`) to `go.mod`.

Focused verification:

`GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database ./internal/bridge -count=1`

Result: passed.

Full verification:

`GOCACHE=/private/tmp/devkit-go-cache go test ./... -count=1`

Result: passed.

`main.go` defers `container.Database.Close()` after container construction, so
all managed PostgreSQL pools close when the Wails application exits.
