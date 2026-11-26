//go:build !unix

package config

// lock is a no-op where flock is unavailable. Saves are still atomic, so
// the file is never corrupted; concurrent updates may lose one change.
func lock(string) (func(), error) { return func() {}, nil }
