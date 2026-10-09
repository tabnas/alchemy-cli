//! The cross product `alchemy translate` answers for: every document of
//! the corpora, read with its format's grammar, written in every format
//! the command carries, read back with that format's grammar, and compared
//! with the document's own value under the target's declared conventions
//! (its loss list). A pair that fails to write, that writes a document its
//! own grammar refuses, or that reads back as anything but the conventions
//! say is a failure, and so is a corpus that shrinks.
//!
//! The corpora are the sibling checkouts': transduce's fixtures (aless's,
//! one document per format at least, and more for YAML and ZON) and the
//! documents of JSONTestSuite every JSON parser must accept (jsonc's
//! conformance pins). A fixture its own grammar refuses (ZON's repeated
//! fields) is no document, and is counted as one refused.

use std::io::Write;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};

use tabnas_alchemy::translate::Options;
use tabnas_alchemy_cli::translate::{self, Format, Request};
use tabnas_transduce::{Datum, Fail, Limits, Metrics};

/// What a run wrote, shared with the test.
#[derive(Clone, Default)]
struct Buffer(Arc<Mutex<Vec<u8>>>);

impl Write for Buffer {
    fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
        self.0.lock().unwrap().extend_from_slice(bytes);
        Ok(bytes.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

/// The directory the sibling checkouts are in.
fn siblings() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../..")
}

/// The format a fixture's extension names, by its id.
fn format_of(extension: &str) -> Option<&'static str> {
    Some(match extension {
        "json" => "json",
        "json5" => "json5",
        "jsonc" => "jsonc",
        "jsonic" => "jsonic",
        "jsonl" => "jsonl",
        "csv" => "csv",
        "md" => "markdown",
        "toml" => "toml",
        "ini" => "ini",
        "xml" => "xml",
        "yaml" => "yaml",
        "zon" => "zon",
        _ => return None,
    })
}

/// Every document of the corpora: its name, its format and its text.
fn corpus() -> Vec<(String, &'static str, String)> {
    let mut docs = Vec::new();
    let dirs = [
        (
            "transduce",
            siblings().join("transduce/rs/tests/fixtures"),
            None,
        ),
        (
            "JSONTestSuite",
            siblings().join("jsonc/test/JSONTestSuite/test_parsing"),
            Some("y_"),
        ),
    ];
    for (corpus, dir, prefix) in dirs {
        let mut entries: Vec<PathBuf> = std::fs::read_dir(&dir)
            .unwrap_or_else(|e| panic!("{corpus}: cannot read {}: {e}", dir.display()))
            .map(|e| e.unwrap().path())
            .collect();
        entries.sort();
        for path in entries {
            let name = path.file_name().unwrap().to_string_lossy().to_string();
            if prefix.is_some_and(|p| !name.starts_with(p)) {
                continue;
            }
            let Some(id) = path
                .extension()
                .and_then(|e| e.to_str())
                .and_then(format_of)
            else {
                continue;
            };
            let Ok(text) = std::fs::read_to_string(&path) else {
                continue;
            };
            docs.push((format!("{corpus}/{name}"), id, text));
        }
    }
    docs
}

fn format(id: &str) -> &'static Format {
    translate::format(id).unwrap_or_else(|| panic!("{id} is a format"))
}

/// Translate `text`, read as `from`, into `to`, with `program` in front
/// when there is one.
fn translate_text(
    from: &Format,
    to: &Format,
    text: &str,
    program: Option<(&str, &str)>,
) -> Result<String, Fail> {
    let request = Request {
        from,
        to,
        path: None,
        options: Options::default(),
        program,
        limits: Limits::default(),
    };
    let out = Buffer::default();
    translate::run(&request, text, Box::new(out.clone()), Metrics::new())?;
    let bytes = out.0.lock().unwrap().clone();
    Ok(String::from_utf8(bytes).expect("a translation writes UTF-8"))
}

// ---------------------------------------------------------------------
// Values compared as the conventions compare them
// ---------------------------------------------------------------------

/// Two values the same: numbers by value (NaN is NaN), objects by their
/// members whatever their order, arrays in order.
fn same(a: &Datum, b: &Datum) -> bool {
    match (a, b) {
        (Datum::Number { value: x, .. }, Datum::Number { value: y, .. }) => {
            x == y || (x.is_nan() && y.is_nan())
        }
        (Datum::Array(x), Datum::Array(y)) => {
            x.len() == y.len() && x.iter().zip(y).all(|(a, b)| same(a, b))
        }
        (Datum::Object(x), Datum::Object(y)) => {
            x.len() == y.len() && x.iter().all(|(k, v)| y.get(k).is_some_and(|w| same(v, w)))
        }
        (a, b) => a == b,
    }
}

/// The value with every number that is not finite replaced.
fn map_non_finite(d: &Datum, f: &dyn Fn(f64) -> Datum) -> Datum {
    match d {
        Datum::Number { value, .. } if !value.is_finite() => f(*value),
        Datum::Array(items) => Datum::Array(items.iter().map(|i| map_non_finite(i, f)).collect()),
        Datum::Object(members) => Datum::Object(
            members
                .iter()
                .map(|(k, v)| (k.clone(), map_non_finite(v, f)))
                .collect(),
        ),
        d => d.clone(),
    }
}

fn wrap_object(d: &Datum, key: &str) -> Datum {
    match d {
        Datum::Object(_) => d.clone(),
        d => Datum::Object([(key.into(), d.clone())].into_iter().collect()),
    }
}

fn wrap_array(d: &Datum) -> Datum {
    match d {
        Datum::Array(_) => d.clone(),
        d => Datum::Array(vec![d.clone()]),
    }
}

/// TOML's conventions: no null (a member whose value is null is not
/// written, a null element is skipped).
fn without_nulls(d: &Datum) -> Datum {
    match d {
        Datum::Array(items) => Datum::Array(
            items
                .iter()
                .filter(|i| !matches!(i, Datum::Null))
                .map(without_nulls)
                .collect(),
        ),
        Datum::Object(members) => Datum::Object(
            members
                .iter()
                .filter(|(_, v)| !matches!(v, Datum::Null))
                .map(|(k, v)| (k.clone(), without_nulls(v)))
                .collect(),
        ),
        d => d.clone(),
    }
}

/// The text a cell is written as in CSV and Markdown: a string as it is,
/// a number by its value (compared as one), a non-finite one by its word,
/// a boolean by its name, null and an absent member as the empty field, a
/// container as its compact JSON text.
enum Cell {
    Text(String),
    Number(f64),
}

fn cell(d: Option<&Datum>) -> Cell {
    match d {
        None | Some(Datum::Null) => Cell::Text(String::new()),
        Some(Datum::Bool(b)) => Cell::Text(b.to_string()),
        Some(Datum::Number { value, .. }) if value.is_nan() => Cell::Text("NaN".into()),
        Some(Datum::Number { value, .. }) if value.is_infinite() => Cell::Text(
            if *value > 0.0 {
                "Infinity"
            } else {
                "-Infinity"
            }
            .into(),
        ),
        Some(Datum::Number { value, .. }) => Cell::Number(*value),
        Some(Datum::String(s)) => Cell::Text(s.to_string()),
        Some(d) => Cell::Text(d.to_string()),
    }
}

fn cell_is(expected: &Cell, got: &str) -> bool {
    match expected {
        Cell::Text(t) => t == got,
        Cell::Number(n) => got.parse::<f64>().is_ok_and(|g| g == *n),
    }
}

/// The table the inferred binding makes of a value: the rows are the
/// root array's elements (a root of another kind is one row), the columns
/// the first row's (an object's keys, an array's positions, or one
/// `value` column for a scalar), each row's cell found by the column's
/// path.
fn table(d: &Datum) -> (Vec<String>, Vec<Vec<Cell>>) {
    let rows = match wrap_array(d) {
        Datum::Array(rows) => rows,
        _ => unreachable!(),
    };
    enum Path {
        Key(String),
        Index(usize),
        Itself,
    }
    let columns: Vec<(String, Path)> = match rows.first() {
        None => Vec::new(),
        Some(Datum::Object(m)) => m
            .keys()
            .map(|k| (k.to_string(), Path::Key(k.to_string())))
            .collect(),
        Some(Datum::Array(items)) => (0..items.len())
            .map(|i| (i.to_string(), Path::Index(i)))
            .collect(),
        Some(_) => vec![("value".to_string(), Path::Itself)],
    };
    let cells = rows
        .iter()
        .map(|row| {
            columns
                .iter()
                .map(|(_, path)| match path {
                    Path::Key(k) => cell(row.as_object().and_then(|m| m.get(k.as_str()))),
                    Path::Index(i) => cell(row.as_array().and_then(|a| a.get(*i))),
                    Path::Itself => cell(Some(row)),
                })
                .collect()
        })
        .collect();
    (columns.into_iter().map(|(l, _)| l).collect(), cells)
}

/// Whether a read-back table (an array of objects keyed by label, every
/// value a string) is the table the inferred binding makes of `source`,
/// with each cell's text passed through `cell_text` first (Markdown's
/// normalisation of what it writes).
fn check_records(
    source: &Datum,
    back: &Datum,
    cell_text: &dyn Fn(&str) -> String,
) -> Result<(), String> {
    let (labels, rows) = table(source);
    let back_rows = back
        .as_array()
        .ok_or_else(|| format!("read back as {back}, not an array of records"))?;
    if labels.is_empty() {
        return if back_rows.is_empty() {
            Ok(())
        } else {
            Err(format!("a table of no columns read back as {back}"))
        };
    }
    if back_rows.len() != rows.len() {
        return Err(format!(
            "{} rows read back, {} written: {back}",
            back_rows.len(),
            rows.len()
        ));
    }
    for (i, (row, got)) in rows.iter().zip(back_rows).enumerate() {
        let got = got
            .as_object()
            .ok_or_else(|| format!("row {i} read back as {got}"))?;
        for (label, expected) in labels.iter().zip(row) {
            let text = match got.get(label.as_str()) {
                Some(Datum::String(s)) => s.to_string(),
                Some(Datum::Null) | None => String::new(),
                Some(other) => other.to_string(),
            };
            let expected = match expected {
                Cell::Text(t) => Cell::Text(cell_text(t)),
                Cell::Number(n) => Cell::Number(*n),
            };
            if !cell_is(&expected, &text) {
                return Err(format!(
                    "row {i}, column {label:?}: read back {text:?}, where {} was written",
                    match expected {
                        Cell::Text(t) => format!("{t:?}"),
                        Cell::Number(n) => n.to_string(),
                    }
                ));
            }
        }
    }
    Ok(())
}

/// Markdown's normalisation of a written cell: a line break is a space,
/// and the spaces at either end are not kept.
fn markdown_cell(text: &str) -> String {
    text.replace("\r\n", " ")
        .replace(['\n', '\r'], " ")
        .trim()
        .to_string()
}

// ---------------------------------------------------------------------
// The cross product
// ---------------------------------------------------------------------

/// Whether an INI document read back (`back`) is what INI's conventions
/// make of `expected`: an object is a section (or the root) and an array
/// of scalars is `key[]` lines, each read back as itself; a number reads
/// back as its text, and one that is not finite as its word; a container
/// INI has no place for (inside an array, an empty array, or under a key
/// no header can spell) reads back as its compact JSON text, a string;
/// true, false and null read back as themselves, and a string as itself.
fn ini_same(expected: &Datum, back: &Datum) -> bool {
    match (expected, back) {
        (Datum::Object(e), Datum::Object(b)) => {
            e.len() == b.len()
                && e.iter().all(|(k, v)| {
                    b.get(k)
                        .or_else(|| b.get(k.trim()))
                        .is_some_and(|w| ini_same(v, w))
                })
        }
        (Datum::Array(e), Datum::Array(b)) if !e.is_empty() => {
            e.len() == b.len() && e.iter().zip(b).all(|(x, y)| ini_item(x, y))
        }
        (Datum::Number { value, .. }, Datum::String(s)) => number_text_is(*value, s),
        (container @ (Datum::Object(_) | Datum::Array(_)), Datum::String(s)) => {
            json_text_is(container, s)
        }
        (e, b) => same(e, b),
    }
}

/// An array item: a scalar as `ini_same` reads it, a container as its
/// JSON text.
fn ini_item(expected: &Datum, back: &Datum) -> bool {
    match (expected, back) {
        (container @ (Datum::Object(_) | Datum::Array(_)), Datum::String(s)) => {
            json_text_is(container, s)
        }
        (e, b) => ini_same(e, b),
    }
}

/// Whether `text` spells the number `value`: its digits, or the word of
/// one that is not finite.
fn number_text_is(value: f64, text: &str) -> bool {
    match text {
        "Infinity" => value == f64::INFINITY,
        "-Infinity" => value == f64::NEG_INFINITY,
        "NaN" => value.is_nan(),
        t => t.parse::<f64>().is_ok_and(|n| n == value),
    }
}

/// Whether `text` is the compact JSON text of `container`, read back as
/// JSON and compared as values, a number that is not finite matching
/// null or its word.
fn json_text_is(container: &Datum, text: &str) -> bool {
    let Ok(json) = serde_json::from_str::<serde_json::Value>(text) else {
        return false;
    };
    fn matches(d: &Datum, j: &serde_json::Value) -> bool {
        use serde_json::Value as J;
        match (d, j) {
            (Datum::Null, J::Null) | (Datum::Number { .. }, J::Null) => true,
            (Datum::Bool(a), J::Bool(b)) => a == b,
            (Datum::Number { value, .. }, J::Number(n)) => n.as_f64() == Some(*value),
            (Datum::Number { value, .. }, J::String(s)) => number_text_is(*value, s),
            (Datum::String(a), J::String(b)) => **a == **b,
            (Datum::Array(a), J::Array(b)) => {
                a.len() == b.len() && a.iter().zip(b).all(|(x, y)| matches(x, y))
            }
            (Datum::Object(a), J::Object(b)) => {
                a.len() == b.len()
                    && a.iter()
                        .all(|(k, v)| b.get(&**k).is_some_and(|w| matches(v, w)))
            }
            _ => false,
        }
    }
    matches(container, &json)
}

/// ZON's conventions: an empty struct reads back as an empty tuple.
fn zon_reading(d: &Datum) -> Datum {
    match d {
        Datum::Object(m) if m.is_empty() => Datum::Array(Vec::new()),
        Datum::Array(items) => Datum::Array(items.iter().map(zon_reading).collect()),
        Datum::Object(members) => Datum::Object(
            members
                .iter()
                .map(|(k, v)| (k.clone(), zon_reading(v)))
                .collect(),
        ),
        d => d.clone(),
    }
}

/// Whether the document read back from `written` in `target` is what the
/// target's conventions make of `source`, read as `from`.
fn check(from: &Format, target: &Format, source: &Datum, written: &str) -> Result<(), String> {
    let limits = Limits::default();
    let id = target.id();
    // A source whose events carry the target's own schema (XML's element
    // tree into XML) is written as it is, and reads back as it is.
    let embedded = target.part.schema.is_some() && from.part.schema != target.part.schema;
    let back = if id == "markdown" {
        // A Markdown table reads back as records through the format's
        // lift, as a host reads it for a records target.
        let lift = target.part.lift.as_ref().expect("markdown has a lift");
        let program = format!(
            "{}\ndef export [input] (records ({} input))\n",
            lift.text, lift.entry
        );
        let json = translate_text(
            target,
            format("yaml"),
            written,
            Some(("markdown-records.alc", &program)),
        )
        .map_err(|f| format!("the written table does not read back: {f}"))?;
        format("yaml")
            .read(&json, &limits)
            .map_err(|f| format!("its records do not read back: {f}"))?
    } else if id == "xml" && embedded {
        // An embedding reads back through its reverse: the element tree,
        // as JSON, unembedded, and written where a non-finite number has
        // a spelling.
        let tree = target
            .read(written, &limits)
            .map_err(|f| format!("the written document does not read back: {f}"))?;
        let embed = target.part.embed.as_ref().expect("xml has an embed");
        let program = format!("{}\ndef export [input] (xml-unembed input)\n", embed.text);
        let yaml = translate_text(
            format("json"),
            format("yaml"),
            &tree.to_string(),
            Some(("xml-unembed.alc", &program)),
        )
        .map_err(|f| format!("the element tree does not unembed: {f}"))?;
        format("yaml")
            .read(&yaml, &limits)
            .map_err(|f| format!("the unembedded tree does not read back: {f}"))?
    } else {
        target
            .read(written, &limits)
            .map_err(|f| format!("the written document does not read back: {f}"))?
    };
    let key = Options::default().key;
    let expected = match id {
        "csv" => return check_records(source, &back, &|t| t.to_string()),
        "markdown" => return check_records(source, &back, &markdown_cell),
        "ini" => {
            let expected = wrap_object(source, &Options::default().key);
            return if ini_same(&expected, &back) {
                Ok(())
            } else {
                Err(format!("read back as {back}, where {expected} was written"))
            };
        }
        "json" | "jsonc" | "jsonic" => map_non_finite(source, &|_| Datum::Null),
        "jsonl" => map_non_finite(&wrap_array(source), &|_| Datum::Null),
        "toml" => without_nulls(&wrap_object(source, &key)),
        "zon" => zon_reading(source),
        _ => source.clone(),
    };
    if same(&expected, &back) {
        Ok(())
    } else {
        Err(format!("read back as {back}, where {expected} was written"))
    }
}

#[test]
fn every_document_translates_into_every_format() {
    let limits = Limits::default();
    let docs = corpus();
    let targets = translate::formats();
    assert_eq!(targets.len(), 12, "the formats: {}", translate::names());
    let total = docs.len() * targets.len();
    let mut failures: Vec<String> = Vec::new();
    let mut refused_sources = Vec::new();
    let mut pairs = 0;
    for (n, (name, from, text)) in docs.iter().enumerate() {
        let from = format(from);
        let source = match from.read(text, &limits) {
            Ok(d) => d,
            Err(f) => {
                refused_sources.push(format!("{name}: {f}"));
                continue;
            }
        };
        for to in targets {
            pairs += 1;
            let outcome = translate_text(from, to, text, None)
                .map_err(|f| format!("does not write: {f}"))
                .and_then(|written| {
                    // A records source read through its lift writes its
                    // table, not its tree: compare with the table.
                    let lifted = from.part.reads.first()
                        == Some(&tabnas_alchemy::translate::Shape::Records)
                        && to.part.writes == tabnas_alchemy::translate::Shape::Records;
                    if lifted {
                        let back = to
                            .read(&written, &limits)
                            .map(|_| ())
                            .map_err(|f| format!("the written document does not read back: {f}"));
                        return back;
                    }
                    check(from, to, &source, &written)
                });
            if let Err(why) = outcome {
                failures.push(format!("{name} ({}) -> {}: {why}", from.id(), to.id()));
            }
        }
        eprintln!(
            "matrix: {} of {} documents ({}%), {} failures",
            n + 1,
            docs.len(),
            (n + 1) * 100 / docs.len(),
            failures.len()
        );
    }
    for line in &refused_sources {
        eprintln!("refused source: {line}");
    }
    for line in &failures {
        eprintln!("FAIL {line}");
    }
    assert!(
        pairs + refused_sources.len() * targets.len() == total && total >= 120 * 12,
        "the corpora shrank: {} documents",
        docs.len()
    );
    assert!(
        failures.is_empty(),
        "{} of {pairs} pairs failed (above)",
        failures.len()
    );
}
