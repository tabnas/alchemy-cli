/* Copyright (c) 2026 tabnas, MIT License */

//! The crate's version must equal `ts/package.json` "version". The binary's
//! `VERSION` is `env!("CARGO_PKG_VERSION")`, the same value this test reads,
//! so a bump that updates `ts/package.json` and forgets `rs/Cargo.toml` (or
//! the reverse) fails here rather than shipping a crate whose version
//! disagrees with the package it is a port of. Mirrors
//! `ts/test/version.test.ts` and `go/cmd/alchemy/version_test.go`.

use std::fs;
use std::path::Path;

#[test]
fn version_matches_package_json() {
    let path = Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("rs/ has a parent")
        .join("ts")
        .join("package.json");
    let text = fs::read_to_string(&path).expect("ts/package.json is readable");
    let package: serde_json::Value = serde_json::from_str(&text).expect("ts/package.json is JSON");
    assert_eq!(
        package["version"].as_str(),
        Some(env!("CARGO_PKG_VERSION")),
        "VERSION drift: rs/Cargo.toml and ts/package.json disagree. Both are \
         rewritten by admin/publish.sh at release; if you bumped one by hand, \
         bump the other."
    );
}
