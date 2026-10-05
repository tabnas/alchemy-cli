// Copyright (c) 2026 tabnas, MIT License

package main

// The baked-in VERSION must equal the package's declared version. A release
// that bumps ts/package.json and forgets this const fails here instead of
// shipping a module that misstates its own version. Mirrors
// ts/test/version.test.ts and rs/tests/version_test.rs.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionMatchesPackageJSON(t *testing.T) {
	// This test lives in go/cmd/alchemy, three levels below the repo root.
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "ts", "package.json"))
	if err != nil {
		// Fatal, never skipped: a version check that silently does not run
		// is the failure this test exists to prevent.
		t.Fatalf("cannot read ts/package.json, so VERSION cannot be checked: %v", err)
	}
	var pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("ts/package.json is not readable JSON: %v", err)
	}
	if pkg.Version == "" {
		t.Fatal("ts/package.json has no version field")
	}
	if VERSION != pkg.Version {
		t.Errorf("VERSION drift: go VERSION = %q but %s package.json = %q.\n"+
			"Both are rewritten by admin/publish.sh at release; if you bumped one by "+
			"hand, bump the other.", VERSION, pkg.Name, pkg.Version)
	}
}
