//! `alchemy translate`: a document in one of the formats whose grammar
//! packages this command carries, written in any of them.
//!
//! Every format's package exports its translation parts: its manifest's
//! `translate` object (the shapes it reads and writes, the root its render
//! needs, the schema its events carry), its render, and, where it has
//! them, its lift and its embedding, each an alchemy file. This module
//! reads them into alchemy's [`Part`]s, has alchemy's `translate` module
//! compose the route (the source's lift, the root adapters, the
//! embedding, the inferred table, the render), compiles the composition
//! with transduce's routers and render's renderers, and runs it over the
//! input read with the source format's grammar: incrementally where
//! transduce's differential suite has verified that grammar
//! ([`capability::incremental`]), so that a number keeps the lexeme the
//! document spelled it with, and materialized otherwise, when a path
//! selects a value below the root, and for a document whose value the
//! grammar builds otherwise than its events showed (a YAML stream of
//! several documents, a merge key, a repeated member), which the
//! incremental source refuses part way: the grammar's own value is the
//! document, so it is read again whole. Nothing here knows a
//! format by its name: a format is what its manifest says, and a package
//! whose manifest names no parts this host can take is not a format here.

use std::io::Write;
use std::sync::{Arc, OnceLock};

use tabnas_alchemy::translate::{
    compose, compose_program, Composition, Descriptor, Front, Options, Part, PartText, Render,
    Root, Shape,
};
use tabnas_alchemy::{Output, Program, Source};
use tabnas_transduce::{
    capability, walk_datum, Code, Datum, DatumBuilder, Duplicates, Fail, Flow, JsonEvent, Limits,
    Metrics, ParserSource, Prune, Segment, Sink, SourceMode, TreeContract,
};

/// The parser a format's documents are read with, as a source over a
/// text.
type Reader = for<'s> fn(&'s str) -> ParserSource<'s>;

/// One format this command reads and writes: its parts, as its package's
/// manifest names them, and the grammar its documents are read with.
pub struct Format {
    pub part: Part,
    reader: Reader,
}

impl Format {
    /// The manifest's `languageId`.
    pub fn id(&self) -> &str {
        &self.part.id
    }

    /// A document read whole with this format's grammar, as a value: its
    /// events as `translate` reads them, incrementally where the grammar
    /// is verified (so a number keeps the lexeme the document spelled it
    /// with), collected, or the grammar's value where the incremental
    /// source cannot follow it. A repeated member keeps its last value.
    pub fn read(&self, input: &str, limits: &Limits) -> Result<Datum, Fail> {
        if capability::incremental(self.id()) {
            match read(
                self,
                input,
                SourceMode::Incremental {
                    prune: Prune::Never,
                },
                limits,
            ) {
                Err(fail) if unfollowed(&fail) => {}
                outcome => return outcome,
            }
        }
        read(self, input, SourceMode::Materialize, limits)
    }
}

/// A package's parts as alchemy's descriptor. Every grammar package owns
/// its interface types, so this reads the same fields from each one's and
/// turns them into alchemy's at once; a package whose parts carry an
/// embedding says so with `embed`.
macro_rules! descriptor {
    ($package:literal, $module:ident) => {
        $module::translate().map(|parts| Descriptor {
            package: $package.to_string(),
            manifest: parts.manifest.to_string(),
            lift: parts.lift.map(|p| PartText::new(p.entry, p.source)),
            embed: None,
            render: parts.render.map(|p| PartText::new(p.entry, p.source)),
        })
    };
    ($package:literal, $module:ident, embed) => {
        $module::translate().map(|parts| Descriptor {
            package: $package.to_string(),
            manifest: parts.manifest.to_string(),
            lift: parts.lift.map(|p| PartText::new(p.entry, p.source)),
            embed: parts.embed.map(|p| PartText::new(p.entry, p.source)),
            render: parts.render.map(|p| PartText::new(p.entry, p.source)),
        })
    };
}

/// The reader of a package's grammar with its own options.
macro_rules! reader {
    ($module:ident) => {{
        fn read(text: &str) -> ParserSource<'_> {
            ParserSource::new($module::make(), text)
        }
        read as Reader
    }};
}

/// Every grammar package this command carries, with its reader.
fn packages() -> Vec<(Option<Descriptor>, Reader)> {
    vec![
        (descriptor!("tabnas-csv", tabnas_csv), reader!(tabnas_csv)),
        (descriptor!("tabnas-ini", tabnas_ini), reader!(tabnas_ini)),
        (
            descriptor!("tabnas-json", tabnas_json),
            reader!(tabnas_json),
        ),
        (
            descriptor!("tabnas-json5", tabnas_json5),
            reader!(tabnas_json5),
        ),
        (
            descriptor!("tabnas-jsonc", tabnas_jsonc),
            reader!(tabnas_jsonc),
        ),
        (
            descriptor!("tabnas-jsonic", tabnas_jsonic),
            reader!(tabnas_jsonic),
        ),
        (
            descriptor!("tabnas-jsonl", tabnas_jsonl),
            reader!(tabnas_jsonl),
        ),
        (
            descriptor!("tabnas-markdown", tabnas_markdown),
            reader!(tabnas_markdown),
        ),
        (
            descriptor!("tabnas-toml", tabnas_toml, embed),
            reader!(tabnas_toml),
        ),
        (
            descriptor!("tabnas-xml", tabnas_xml, embed),
            reader!(tabnas_xml),
        ),
        (
            descriptor!("tabnas-yaml", tabnas_yaml),
            reader!(tabnas_yaml),
        ),
        (descriptor!("tabnas-zon", tabnas_zon), reader!(tabnas_zon)),
    ]
}

/// Every format, by id, read once.
pub fn formats() -> &'static [Format] {
    static FORMATS: OnceLock<Vec<Format>> = OnceLock::new();
    FORMATS.get_or_init(|| {
        let mut formats: Vec<Format> = packages()
            .into_iter()
            .filter_map(|(descriptor, reader)| {
                Some(Format {
                    part: Part::from_descriptor(&descriptor?)?,
                    reader,
                })
            })
            .collect();
        formats.sort_by(|a, b| a.part.id.cmp(&b.part.id));
        formats
    })
}

/// The format a command line names, by its id.
pub fn format(id: &str) -> Option<&'static Format> {
    formats().iter().find(|f| f.id() == id)
}

/// The formats' ids for a message: `csv, ini, ... or zon`.
pub fn names() -> String {
    let ids: Vec<&str> = formats().iter().map(Format::id).collect();
    match ids.split_last() {
        Some((last, [])) => last.to_string(),
        Some((last, rest)) => format!("{} or {last}", rest.join(", ")),
        None => String::new(),
    }
}

/// What a translation was asked.
pub struct Request<'a> {
    /// The format the input is read with.
    pub from: &'a Format,
    /// The format it is written in.
    pub to: &'a Format,
    /// A value below the root to translate instead of the whole document,
    /// as segments: a plain tree, whatever the source's shapes.
    pub path: Option<Vec<Segment>>,
    /// What the host chooses: the key a root is wrapped under.
    pub options: Options,
    /// A program, as its file name and its text, whose export stands in
    /// the source's place: its JSON events a tree, its table records.
    pub program: Option<(&'a str, &'a str)>,
    pub limits: Limits,
}

/// The composition a request makes, and the program it compiles to.
pub fn compile(request: &Request<'_>) -> Result<(Composition, Program), Fail> {
    let routers = Arc::new(tabnas_transduce::routers());
    let renderers = Arc::new(tabnas_render::renderers());
    match request.program {
        Some((file, text)) => {
            let output =
                tabnas_alchemy::compile(text, file, routers.clone(), renderers.clone())?.output();
            if output == Output::Text {
                return Err(Fail::new(
                    Code::DslTypeError,
                    format!(
                        "bad_output: {file}'s export writes its own text, and translate takes a \
                         program whose export answers JSON events or a table, which a format's \
                         render then writes"
                    ),
                ));
            }
            let composition =
                compose_program(output, &request.to.part, &request.options, "translate")?;
            let program = composition.compile(Some(Source::new(file, text)), routers, renderers)?;
            Ok((composition, program))
        }
        None => {
            let source = match request.path {
                Some(_) => None,
                None => Some(&request.from.part),
            };
            let composition = compose(source, &request.to.part, &request.options, "translate")?;
            let program = composition.compile(None, routers, renderers)?;
            Ok((composition, program))
        }
    }
}

/// Whether the incremental source refused a document because the
/// grammar's value is not what its events showed (a root wrapped or
/// replaced after it streamed, a map rewritten, a repeated member that the
/// grammar keeps once): the grammar's own value is the document, so it is
/// read again whole.
fn unfollowed(fail: &Fail) -> bool {
    matches!(
        fail.code,
        Code::StreamabilityUnknown | Code::DuplicateMember
    )
}

/// What a run writes, held until the run has succeeded, so that a run
/// the incremental source gives up part way is run again from the start
/// rather than written twice.
#[derive(Clone, Default)]
struct Held(Arc<std::sync::Mutex<Vec<u8>>>);

impl Write for Held {
    fn write(&mut self, bytes: &[u8]) -> std::io::Result<usize> {
        self.0
            .lock()
            .map_err(|_| std::io::Error::other("the held output's lock was poisoned"))?
            .extend_from_slice(bytes);
        Ok(bytes.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

/// Run a request over `input`, writing the document to `out` once the
/// run has succeeded; `metrics` collects what the stages report, the
/// output's bytes among it.
pub fn run(
    request: &Request<'_>,
    input: &str,
    mut out: Box<dyn Write + Send>,
    metrics: Arc<Metrics>,
) -> Result<(), Fail> {
    let (composition, program) = compile(request)?;
    let attempt = |mode: SourceMode, metrics: Arc<Metrics>| -> Result<Vec<u8>, Fail> {
        let held = Held::default();
        let sink = program.sink(
            Box::new(held.clone()),
            None,
            &request.limits,
            metrics.clone(),
        )?;
        // The source's events reach a tree's render as the source made
        // them, so a stream that is no tree's is refused in front of it
        // rather than written half way.
        let mut sink: Box<dyn Sink + Send> = match composition.front {
            Front::Tree => Box::new(TreeContract::new(sink)),
            _ => sink,
        };
        match &request.path {
            None => {
                (request.from.reader)(input)
                    .grammar(request.from.id())
                    .mode(mode)
                    .limits(request.limits.clone())
                    .metrics(metrics)
                    .run_owned(sink)
                    .0?;
            }
            Some(path) => {
                let value = read(
                    request.from,
                    input,
                    SourceMode::Materialize,
                    &request.limits,
                )?;
                let selected = value.get_path(path).ok_or_else(|| {
                    Fail::input(format!(
                        "the path {} names nothing in the document",
                        path_text(path)
                    ))
                })?;
                if walk_datum(selected, &mut sink)? == Flow::Continue {
                    sink.event(JsonEvent::End)?;
                }
            }
        }
        let bytes =
            std::mem::take(&mut *held.0.lock().map_err(|_| {
                Fail::new(Code::OutputFailed, "the held output's lock was poisoned")
            })?);
        Ok(bytes)
    };
    let incremental = request.path.is_none() && capability::incremental(request.from.id());
    let bytes = if incremental {
        let mode = SourceMode::Incremental {
            prune: program
                .row_selector()
                .map_or(Prune::Never, |s| Prune::Under(s.clone())),
        };
        match attempt(mode, Metrics::new()) {
            Err(fail) if unfollowed(&fail) => attempt(SourceMode::Materialize, metrics)?,
            outcome => outcome?,
        }
    } else {
        attempt(SourceMode::Materialize, metrics)?
    };
    out.write_all(&bytes)
        .and_then(|()| out.flush())
        .map_err(|e| {
            Fail::new(
                Code::OutputFailed,
                format!("the output could not be written: {e}"),
            )
        })
}

/// The events of a document, collected into a value.
struct Collect(DatumBuilder);

impl Sink for Collect {
    fn event(&mut self, ev: JsonEvent<'_>) -> Result<Flow, Fail> {
        if !matches!(ev, JsonEvent::End) {
            self.0.event(ev)?;
        }
        Ok(Flow::Continue)
    }
}

/// A document read whole with a format's grammar, as a value. A repeated
/// member keeps its last value, as the grammar's own value does.
fn read(format: &Format, input: &str, mode: SourceMode, limits: &Limits) -> Result<Datum, Fail> {
    let collect = Collect(DatumBuilder::new(
        usize::MAX,
        "max_capture_bytes",
        Duplicates::LastWins,
    ));
    let (outcome, mut collect) = (format.reader)(input)
        .grammar(format.id())
        .mode(mode)
        .limits(limits.clone())
        .run_owned(collect);
    outcome?;
    collect
        .0
        .take()
        .ok_or_else(|| Fail::input("the document holds no value"))
}

/// A path given as a JSON array of segments, each a string (a member's
/// key) or a whole number (an element's index), the form alchemy's
/// `as-path` validates.
pub fn parse_path(text: &str, limits: &Limits) -> Result<Vec<Segment>, Fail> {
    let json = format("json").ok_or_else(|| Fail::input("no JSON grammar to read the path"))?;
    let refuse = || {
        Fail::input(format!(
            "--path takes a JSON array of keys and indexes, such as [\"people\",0], not {text}"
        ))
    };
    let value = json.read(text, limits).map_err(|_| refuse())?;
    let Some(items) = value.as_array() else {
        return Err(refuse());
    };
    items
        .iter()
        .map(|item| match item {
            Datum::String(key) => Ok(Segment::Key(key.clone())),
            Datum::Number { value, .. }
                if value.fract() == 0.0 && *value >= 0.0 && *value <= usize::MAX as f64 =>
            {
                Ok(Segment::Index(*value as usize))
            }
            _ => Err(refuse()),
        })
        .collect()
}

/// A path as the JSON array it was given as.
fn path_text(path: &[Segment]) -> String {
    let items: Vec<Datum> = path
        .iter()
        .map(|s| match s {
            Segment::Key(k) => Datum::String(k.clone()),
            Segment::Index(i) => Datum::Number {
                value: *i as f64,
                lexeme: Some(i.to_string().into()),
            },
        })
        .collect();
    Datum::Array(items).to_string()
}

/// The registry as JSON, one object per format, for `alchemy formats`:
/// its id, the shapes it reads and writes, the root its render needs, its
/// schema, its parts' entries, and its loss sentences.
pub fn formats_json() -> String {
    let shape = |s: &Shape| {
        Datum::String(
            match s {
                Shape::Tree => "tree",
                Shape::Records => "records",
            }
            .into(),
        )
    };
    let text = |s: &str| Datum::String(s.into());
    let items: Vec<Datum> = formats()
        .iter()
        .map(|f| {
            let p = &f.part;
            let fields: [(&str, Datum); 9] = [
                ("id", text(&p.id)),
                ("reads", Datum::Array(p.reads.iter().map(shape).collect())),
                ("writes", shape(&p.writes)),
                (
                    "root",
                    text(match p.root {
                        Root::Object => "object",
                        Root::Array => "array",
                        Root::Any => "any",
                    }),
                ),
                ("schema", p.schema.as_deref().map_or(Datum::Null, text)),
                (
                    "lift",
                    p.lift.as_ref().map_or(Datum::Null, |a| text(&a.entry)),
                ),
                (
                    "embed",
                    p.embed.as_ref().map_or(Datum::Null, |a| text(&a.entry)),
                ),
                (
                    "render",
                    text(match &p.render {
                        Render::Alc(a) => &a.entry,
                        Render::Json => "json",
                        Render::Csv => "csv",
                    }),
                ),
                (
                    "loss",
                    Datum::Array(p.loss.iter().map(|l| text(l)).collect()),
                ),
            ];
            Datum::Object(fields.into_iter().map(|(k, v)| (k.into(), v)).collect())
        })
        .collect();
    Datum::Array(items).to_string()
}
