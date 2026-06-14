//go:build ignore

package main

import (
	"log/slog"
	"os"

	"github.com/pgaskin/ottrec-website/internal/npm"
)

// Downloads the npm packages locked in ../package-lock.json and writes each as
// a txtar archive under lib/ (see internal/npm). Run via `go generate ./static`.
func main() {
	lock, err := os.ReadFile("../package-lock.json")
	if err != nil {
		slog.Error("failed to read lockfile", "error", err)
		os.Exit(1)
	}
	if err := npm.Vendor(lock, "lib"); err != nil {
		slog.Error("failed to vendor npm packages", "error", err)
		os.Exit(1)
	}
	slog.Info("vendored npm packages into lib/")
}
