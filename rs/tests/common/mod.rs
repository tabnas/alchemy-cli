// Shared helpers of the tests moved here from tabnas-alchemy: the tests
// that lower and run a program need real routers and renderers, which
// alchemy does not depend on, and this crate, the composition root, does.
// Cargo compiles this module into EVERY integration test binary, so an
// item only one binary uses is dead code in the others; the allow keeps
// that from being a warning rather than hiding anything real.
#![allow(dead_code)]

use std::path::{Path, PathBuf};
use std::sync::Arc;

use tabnas_alchemy::shared::{Fail, Renderers, Routers};
use tabnas_alchemy::value::Val;
use tabnas_alchemy::{Program, Source};
use tabnas_support::Value;

/// The routers a host passes alchemy: transduce's.
pub fn routers() -> Arc<dyn Routers<Val>> {
    Arc::new(tabnas_transduce::routers())
}

/// The renderers a host passes alchemy: render's.
pub fn renderers() -> Arc<dyn Renderers> {
    Arc::new(tabnas_render::renderers())
}

/// [`tabnas_alchemy::compile`] with transduce's routers and render's
/// renderers, as a host compiles.
pub fn compile(src: &str, file: &str) -> Result<Program, Fail> {
    tabnas_alchemy::compile(src, file, routers(), renderers())
}

/// [`tabnas_alchemy::compile_sources`] with transduce's routers and
/// render's renderers.
pub fn compile_sources(sources: &[Source<'_>]) -> Result<Program, Fail> {
    tabnas_alchemy::compile_sources(sources, routers(), renderers())
}

/// This repository's root: the parent of `rs/`.
pub fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .parent()
        .expect("rs/ has a parent")
        .to_path_buf()
}

/// The alchemy checkout beside this repository, which `rs/Cargo.toml`
/// already takes the crate from by path: the tests read its shared
/// fixtures. A missing checkout fails, naming where it was looked for.
pub fn alchemy_root() -> PathBuf {
    let root = repo_root()
        .parent()
        .expect("the repository has a parent")
        .join("alchemy");
    assert!(
        root.join("test/spec").is_dir(),
        "no alchemy checkout with test/spec at {}: clone https://github.com/tabnas/alchemy \
         as a sibling of this repository",
        root.display()
    );
    root
}

/// alchemy's shared `test/spec` directory, in the sibling checkout.
pub fn spec_dir() -> PathBuf {
    alchemy_root().join("test/spec")
}

/// The code a `Fail` from alchemy pins: the reader and the desugarer
/// both lead the message with it (`bad_dedent: ...`, `empty_step: ...`),
/// so `ERROR:<code>` in a fixture names that word. A message without the
/// convention pins nothing but the transduce code.
pub fn fail_code(fail: &Fail) -> String {
    fail.message.split_once(": ").map_or_else(
        || fail.code.as_str().to_string(),
        |(code, _)| code.to_string(),
    )
}

/// Canonical text as the fixture data model: the expected column is a
/// JSON string of it.
pub fn text_value(text: String) -> Value {
    Value::String(text)
}
