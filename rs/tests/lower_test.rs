// The tests that lower a program and run it through transduce's routers
// and render's renderers, moved first from the unit tests of tabnas-alchemy's
// src/lower.rs, src/program.rs and src/effects.rs (a unit test in src/
// cannot take the real routers and renderers, which are built on alchemy's
// library, of which a unit test is a second copy) to alchemy's tests/, and
// then here, since alchemy depends on neither transduce nor render. Each
// module is named for the alchemy source file its tests were in.

mod common;

use std::sync::{Arc, Mutex};

use common::{compile, json_into, renderers, routers};
use tabnas_alchemy::ast::Sources;
use tabnas_alchemy::interp::Runtime;
use tabnas_alchemy::lower::{Lowering, Out, Renderer};
use tabnas_alchemy::resolve::resolve;
use tabnas_alchemy::shared::{Fail, Limits, Metrics};
use tabnas_alchemy::{desugar, parse_file, stdlib};

/// The spec's worked example, byte for byte as aless's fixture has it.
const RECORDS: &str = r#"{"response":{"metadata":{"fields":[{"title":"Identifier","path":["id"]},{"title":"Full name","path":["person","name"]},{"title":"Balance","path":["account","balance"]}]},"payload":{"deep":{"records":[{"id":123,"person":{"name":"Alice"},"account":{"balance":50.25}},{"account":{"balance":72},"person":{"name":"Bob"},"id":456}]}}}}"#;

const EXPECTED_CSV: &str =
    "\"Identifier\",\"Full name\",\"Balance\"\r\n\"123\",\"Alice\",\"50.25\"\r\n\"456\",\"Bob\",\"72\"\r\n";

/// The spec's program: sections 12.1 and 13.4.
const PROGRAM: &str = "def column-from-meta [source]\n  record\n    entry :label (get \"title\" source)\n    entry :source\n      as-path\n        get \"path\" source\n\ndef api-binding\n  record\n    entry :columns\n      path \"response\" \"metadata\" \"fields\"\n    entry :rows\n      path \"response\" \"payload\" \"deep\" \"records\" each-index\n    entry :column column-from-meta\n\ndef api-table [input]\n  table-from-json api-binding input\n\ndef export [input]\n  pipe input\n    api-table\n    csv csv-options\n";

fn runtime(src: &str) -> Arc<Runtime> {
    let forms = desugar::program(parse_file(src, "t.alc").unwrap(), src).unwrap();
    let sources = Sources::one("t.alc", src);
    let resolved = resolve(forms, &sources, &stdlib::outer).unwrap();
    Arc::new(Runtime::new(Arc::new(resolved), sources))
}

/// A writer the test keeps a handle on after the sink took it.
#[derive(Clone, Default)]
struct Shared(Arc<Mutex<Vec<u8>>>);

impl std::io::Write for Shared {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.0.lock().unwrap().extend_from_slice(buf);
        Ok(buf.len())
    }

    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

/// Run `src` over the JSON `input`, read as `alchemy run` reads its
/// input, lowered through transduce's routers and render's renderers; the
/// output bytes, or the failure.
fn run(src: &str, input: &str, render: Option<Renderer>) -> Result<String, Fail> {
    let rt = runtime(src);
    let result = rt.export()?;
    let limits = Limits::default();
    let metrics = Metrics::new();
    let buffer = Shared::default();
    let out: Out = Box::new(tabnas_render::WriteOut::new(buffer.clone()));
    let sink = Lowering::new(rt, &limits, metrics.clone(), routers(), renderers())
        .sink(&result, out, render)?;
    json_into(input, &limits, metrics, Box::new(sink))?;
    let bytes = buffer.0.lock().unwrap().clone();
    Ok(String::from_utf8(bytes).expect("utf-8 output"))
}

mod lower {
    use super::*;
    use tabnas_alchemy::shared::Code;

    #[test]
    fn the_worked_example_through_the_library() {
        assert_eq!(run(PROGRAM, RECORDS, None).unwrap(), EXPECTED_CSV);
    }

    #[test]
    fn json_echoes_the_input_with_its_lexemes() {
        let echo = "def export [input] (json input)";
        assert_eq!(run(echo, RECORDS, None).unwrap(), format!("{RECORDS}\n"));
        assert_eq!(run(echo, "[1.50, 1e2]", None).unwrap(), "[1.50,1e2]\n");
    }

    #[test]
    fn select_map_and_concat_map_stream_items() {
        let src = "def export [input]\n  pipe input\n    select (path \"a\" each-index)\n    map (fn [x] (get :n x))\n    concat-map (fn [n] (concat (scalar-text csv-options n) \";\"))";
        assert_eq!(
            run(src, r#"{"a":[{"n":1},{"n":2.50}],"b":3}"#, None).unwrap(),
            "1;2.50;"
        );
        let joined = "def export [input]\n  join \",\"\n    map (fn [x] (get :n x)) (select (path \"a\" each-index) input)";
        assert_eq!(
            run(joined, r#"{"a":[{"n":"x"},{"n":""},{"n":"y"}]}"#, None).unwrap(),
            "x,,y"
        );
        let framed = "def export [input]\n  concat\n    \"[\"\n    join \",\" (select (path each-index) input)\n    \"]\"\n    (text \"!\")";
        assert_eq!(run(framed, r#"["a","b"]"#, None).unwrap(), "[a,b]!");
        assert_eq!(run(framed, "[]", None).unwrap(), "[]!");
        let filtered = "def export [input]\n  join \"|\"\n    filter (fn [x] (get :keep x)) (select (path each-index) input)";
        assert_eq!(
            run(
                "def export [input]\n  concat-map (fn [x] (get :v x))\n    filter (fn [x] (get :keep x)) (select (path each-index) input)",
                r#"[{"keep":true,"v":"a"},{"keep":false,"v":"b"},{"keep":true,"v":"c"}]"#,
                None
            )
            .unwrap(),
            "ac"
        );
        let _ = filtered;
    }

    #[test]
    fn replace_text_over_a_live_text_crosses_fragments() {
        let src = "def export [input]\n  replace-text \"ab\" \"X\"\n    concat-map (fn [s] s) (select (path each-index) input)";
        assert_eq!(run(src, r#"["a","b","zab","a"]"#, None).unwrap(), "XzXa");
    }

    #[test]
    fn a_finite_text_result_is_written_at_the_end() {
        assert_eq!(
            run("def export [input] \"done\"", "1", None).unwrap(),
            "done"
        );
    }

    #[test]
    fn a_stream_result_is_rendered_by_the_host() {
        let table = "def export [input] (table-from-json api-binding input)\n".to_string()
            + &PROGRAM.replace(
                "def export [input]\n  pipe input\n    api-table\n    csv csv-options\n",
                "",
            );
        assert_eq!(run(&table, RECORDS, None).unwrap(), EXPECTED_CSV);
        assert_eq!(
            run(&table, RECORDS, Some(Renderer::Csv)).unwrap(),
            EXPECTED_CSV
        );
        let json = r#"[{"Identifier":123,"Full name":"Alice","Balance":50.25},{"Identifier":456,"Full name":"Bob","Balance":72}]"#;
        assert_eq!(
            run(&table, RECORDS, Some(Renderer::Json)).unwrap(),
            format!("{json}\n")
        );
        assert_eq!(
            run(&table, RECORDS, Some(Renderer::Json)).unwrap(),
            format!("{json}\n")
        );
        let echo = "def export [input] input";
        assert_eq!(run(echo, "[1]", None).unwrap(), "[1]\n");
        let f = run(echo, "[1]", Some(Renderer::Csv)).unwrap_err();
        assert!(f.message.starts_with("protocol_mismatch: "), "{f}");
        let f = run("def export [input] (json input)", "1", Some(Renderer::Json)).unwrap_err();
        assert!(f.message.starts_with("render_of_text: "), "{f}");
    }

    #[test]
    fn records_of_a_table_round_trips_through_json() {
        let src = PROGRAM.replace("    csv csv-options\n", "    records\n    json\n");
        let json = r#"[{"Identifier":123,"Full name":"Alice","Balance":50.25},{"Identifier":456,"Full name":"Bob","Balance":72}]"#;
        assert_eq!(run(&src, RECORDS, None).unwrap(), format!("{json}\n"));
        assert_eq!(run(&src, RECORDS, None).unwrap(), format!("{json}\n"));
    }

    #[test]
    fn protocol_mismatches_are_named() {
        let f = run("def export [input] (csv csv-options input)", "1", None).unwrap_err();
        assert_eq!(f.code, Code::DslTypeError);
        assert!(f.message.starts_with("protocol_mismatch: "), "{f}");
        let f = run(
            "def export [input] (json (select (path each-index) input))",
            "[1]",
            None,
        )
        .unwrap_err();
        assert!(f.message.starts_with("protocol_mismatch: "), "{f}");
        let f = run(
            "def export [input] (concat-map (fn [x] x) input)",
            "[1]",
            None,
        )
        .unwrap_err();
        assert!(f.message.starts_with("protocol_mismatch: "), "{f}");
    }
}

mod program {
    use super::*;
    use tabnas_alchemy::shared::Sink;

    #[test]
    fn the_sink_writes_through_a_write() {
        let p = compile(PROGRAM, "t.alc").unwrap();
        let buffer = Shared::default();
        let limits = Limits::default();
        let mut sink = p
            .sink(Box::new(buffer.clone()), None, &limits, Metrics::new())
            .unwrap();
        let datum =
            tabnas_alchemy::shared::Datum::from_json(&serde_json::from_str(RECORDS).unwrap());
        tabnas_alchemy::shared::walk_datum(&datum, &mut sink).unwrap();
        sink.event(tabnas_alchemy::shared::JsonEvent::End).unwrap();
        let bytes = buffer.0.lock().unwrap().clone();
        assert_eq!(String::from_utf8(bytes).unwrap(), EXPECTED_CSV);
    }
}

mod effects {
    use super::*;
    use serde_json::Value as Json;
    use tabnas_alchemy::effects::explain_json;

    /// A program's own `csv` is the library's text, and the report says so:
    /// the renderer is the text's own, and what the text writes is the
    /// dialect its options record names. The host's renderer, for a table
    /// result, is reported with the defaults it is built with.
    #[test]
    fn a_programs_own_csv_renders_as_text_and_the_host_renderer_reports_the_defaults() {
        let lf = PROGRAM.replace("    csv csv-options\n", "    csv lf\n")
            + "\ndef lf (record (entry :delimiter \";\") (entry :newline \"\\n\") (entry :header false) (entry :null-text \"NULL\") (entry :missing \"-\"))\n";
        let program = compile(&lf, "lf.alc").unwrap();
        let r = &explain_json(&program)["renderer"];
        assert_eq!(r["name"], "text");
        assert_eq!(
            run(&lf, RECORDS, None).unwrap(),
            "\"123\";\"Alice\";\"50.25\"\n\"456\";\"Bob\";\"72\"\n"
        );
        // The null and missing texts reach the output as reported: a
        // record with a null name and no balance.
        let sparse = RECORDS.replace(
            r#"{"account":{"balance":72},"person":{"name":"Bob"},"id":456}"#,
            r#"{"person":{"name":null},"id":456}"#,
        );
        assert_ne!(sparse, RECORDS);
        assert_eq!(
            run(&lf, &sparse, None).unwrap(),
            "\"123\";\"Alice\";\"50.25\"\n\"456\";\"NULL\";\"-\"\n"
        );
        // The default dialect is the text's too; the host's renderer reports
        // the defaults it is built with.
        let default = compile(PROGRAM, "export.alc").unwrap();
        assert_eq!(explain_json(&default)["renderer"]["name"], "text");
        let host = compile(&PROGRAM.replace("    csv csv-options\n", ""), "host.alc").unwrap();
        let r = &explain_json(&host)["renderer"];
        assert_eq!(r["name"], "csv");
        assert_eq!(r["host"], true);
        assert_eq!(r["delimiter"], ",");
        assert_eq!(r["newline"], "\r\n");
        assert_eq!(r["header"], true);
        assert_eq!(r["null_text"], "");
        assert_eq!(r["missing"], "error");
        assert_eq!(r["missing_text"], Json::Null);
    }
}
