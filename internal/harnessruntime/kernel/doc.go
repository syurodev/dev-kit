// Package kernel defines the pure, replayable state machine for native harness runs.
// It accepts trusted control facts as typed input and never performs persistence,
// dispatch, authorization, clock reads, random generation, or other I/O.
package kernel
