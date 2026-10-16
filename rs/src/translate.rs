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
//! document, so it is read again whole. Where a package's parser builds a
//! value that is not yet the tree its parts declare, and the package's API
//! reads a document as that tree (expr's simplified form, proto's
//! descriptor, C's realized syntax tree, and the grammar spec a grammar
//! notation's document compiles to), that tree is the document, read
//! whole. A package that ships a transducer part (its manifest's
//! `translate.transduce`, the alchemy file whose entry makes the format's
//! JSON events of the parser's rule passes) is read through it instead:
//! the route is composed over the transducer (`compose_parsed`), and the
//! input's rule passes stream through transduce's `ParserEventSource` into
//! the program, whatever the grammar, with nothing held but what the part
//! keeps; where the part's events are not what the grammar's value holds
//! (a repeated member, which it streams twice and `tree-contract` refuses),
//! the document is read again whole, as above. Nothing here knows a
//! format by its name: a format is what its manifest says, and a package
//! whose manifest names no parts this host can take is not a format here.

use std::io::Write;
use std::sync::{Arc, OnceLock};

use tabnas_alchemy::translate::{
    compose, compose_parsed, compose_program, compose_program_parsed, Alc, Composition, Descriptor,
    Front, Options, Part, PartText, Render, Root, Shape,
};
use tabnas_alchemy::{Output, Program, Source};
use tabnas_transduce::{
    capability, walk_datum, AbortFlag, Code, Datum, DatumBuilder, Duplicates, Fail, Flow, Guarded,
    JsonEvent, Limits, Metrics, ParserEventSource, ParserSource, Prune, Segment, Sink, SourceMode,
    TreeContract,
};

/// A format's grammar applied to a text, two ways, each over a fresh parser
/// of the grammar under the format's own options, since a source owns its
/// parser: as transduce's source of the document's events
/// ([`ParserSource`]), and as its source of the parser's rule passes
/// ([`ParserEventSource`]), which a program over parser events reads (a
/// transducer part, `run --grammar`).
#[derive(Clone, Copy)]
struct Parsing {
    events: for<'s> fn(&'s str) -> ParserSource<'s>,
    passes: for<'s> fn(&'s str) -> ParserEventSource<'s>,
}

/// How a format's documents are read.
#[derive(Clone, Copy)]
enum Reader {
    /// With the grammar's parser.
    Parser(Parsing),
    /// With the grammar's parser, after a check the package's own parse
    /// makes before the parser runs, which the parser does not make
    /// itself: a document the check refuses is not parsed.
    Checked(fn(&str) -> Result<(), Fail>, Parsing),
    /// As the tree the package's own API reads a document as, where its
    /// parser's value is not yet the tree its parts declare: read whole,
    /// and walked as a parser's value is.
    Tree(fn(&str) -> Result<Datum, Fail>),
}

/// One format this command reads and writes: its parts, as its package's
/// manifest names them, the grammar its documents are read with, and the
/// transducer part its package ships, where it ships one.
pub struct Format {
    pub part: Part,
    reader: Reader,
    transducer: Option<Alc>,
}

impl Format {
    /// The manifest's `languageId`.
    pub fn id(&self) -> &str {
        &self.part.id
    }

    /// The transducer part the format's package ships, its manifest's
    /// `translate.transduce`: the alchemy file whose entry makes the
    /// format's JSON events of its parser's rule passes, through which
    /// `translate` reads the format's documents. `None` where the package
    /// ships none, and the documents are read as [`Format::read`] reads
    /// them.
    pub fn transducer(&self) -> Option<&Alc> {
        self.transducer.as_ref()
    }

    /// Whether the format's documents are parsed, so that a program over
    /// parser events can read them ([`Format::passes`]); a format read as
    /// the tree its package's API makes of a document has no parser here.
    pub fn parses(&self) -> bool {
        !matches!(self.reader, Reader::Tree(_))
    }

    /// The parser's rule passes over `text`: transduce's source of
    /// `ParserEvents/1` with a fresh parser of the format's grammar, for a
    /// program over parser events, a transducer part's or `run --grammar`'s.
    /// The check the package's parse makes first, where it makes one, runs
    /// first, and its refusal is the `Err`; a format read as a tree has no
    /// parser, and refuses.
    pub fn passes<'s>(&self, text: &'s str) -> Result<ParserEventSource<'s>, Fail> {
        match self.reader {
            Reader::Parser(parsing) => Ok((parsing.passes)(text)),
            Reader::Checked(check, parsing) => check(text).map(|()| (parsing.passes)(text)),
            Reader::Tree(_) => Err(Fail::input(format!(
                "{} is read as a tree, not parsed: it has no rule passes for a program to read",
                self.id()
            ))),
        }
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

/// A package's parts as alchemy's descriptor, with the transducer part its
/// manifest may name beside it: the descriptor carries none, and
/// [`Part::transducer`] reads the two together.
struct Described {
    descriptor: Descriptor,
    transduce: Option<PartText>,
}

/// A package's parts as alchemy's descriptor. Every grammar package owns
/// its interface types, so this reads the same fields from each one's and
/// turns them into alchemy's at once; a package whose parts carry an
/// embedding says so with `embed`, and one whose parts carry a transducer
/// with `transduce`.
macro_rules! descriptor {
    ($package:literal, $module:ident) => {
        $module::translate().map(|parts| Described {
            descriptor: Descriptor {
                package: $package.to_string(),
                manifest: parts.manifest.to_string(),
                lift: parts.lift.map(|p| PartText::new(p.entry, p.source)),
                embed: None,
                render: parts.render.map(|p| PartText::new(p.entry, p.source)),
            },
            transduce: None,
        })
    };
    ($package:literal, $module:ident, embed) => {
        $module::translate().map(|parts| Described {
            descriptor: Descriptor {
                package: $package.to_string(),
                manifest: parts.manifest.to_string(),
                lift: parts.lift.map(|p| PartText::new(p.entry, p.source)),
                embed: parts.embed.map(|p| PartText::new(p.entry, p.source)),
                render: parts.render.map(|p| PartText::new(p.entry, p.source)),
            },
            transduce: None,
        })
    };
    ($package:literal, $module:ident, transduce) => {
        $module::translate().map(|parts| Described {
            descriptor: Descriptor {
                package: $package.to_string(),
                manifest: parts.manifest.to_string(),
                lift: parts.lift.map(|p| PartText::new(p.entry, p.source)),
                embed: None,
                render: parts.render.map(|p| PartText::new(p.entry, p.source)),
            },
            transduce: parts.transduce.map(|p| PartText::new(p.entry, p.source)),
        })
    };
    ($package:literal, $module:ident, embed, transduce) => {
        $module::translate().map(|parts| Described {
            descriptor: Descriptor {
                package: $package.to_string(),
                manifest: parts.manifest.to_string(),
                lift: parts.lift.map(|p| PartText::new(p.entry, p.source)),
                embed: parts.embed.map(|p| PartText::new(p.entry, p.source)),
                render: parts.render.map(|p| PartText::new(p.entry, p.source)),
            },
            transduce: parts.transduce.map(|p| PartText::new(p.entry, p.source)),
        })
    };
}

/// The two sources of a grammar's parser, each over a fresh parser `$make`
/// makes: the document's events and the parser's rule passes.
macro_rules! parsing {
    ($make:expr) => {{
        fn events(text: &str) -> ParserSource<'_> {
            ParserSource::new($make, text)
        }
        fn passes(text: &str) -> ParserEventSource<'_> {
            ParserEventSource::new($make, text)
        }
        Parsing { events, passes }
    }};
}

/// The reader of a package's grammar with its own options.
macro_rules! reader {
    ($module:ident) => {
        Reader::Parser(parsing!($module::make()))
    };
}

/// The reader of chess's grammar with its default options, which its
/// `make` takes: they always install, so a failure is the package's defect.
fn read_pgn() -> Reader {
    Reader::Parser(parsing!(tabnas_chess::make(
        &tabnas_chess::ChessOptions::default()
    )
    .unwrap_or_else(|e| panic!(
        "tabnas-chess: the grammar does not install: {e}"
    ))))
}

/// json5's check before its parser runs, which its `parse_with` makes and
/// its parser does not: a document holding no value is refused as
/// `json5_empty` or `json5_no_value`, rather than as the engine's
/// `unexpected`. Only a document that may hold none, one that is empty or
/// opens, after whitespace, with a comment, is put to the package's parse.
fn json5_value_check(text: &str) -> Result<(), Fail> {
    let rest = text.trim_start_matches(|c: char| c.is_whitespace() || c == '\u{feff}');
    if !(rest.is_empty() || rest.starts_with('/')) {
        return Ok(());
    }
    match tabnas_json5::parse_with(&tabnas_json5::make(), text) {
        Err(error) if error.code == "json5_empty" || error.code == "json5_no_value" => {
            Err(Fail::from_tabnas(&error))
        }
        _ => Ok(()),
    }
}

/// The reader of json5's grammar, run after [`json5_value_check`].
fn read_json5() -> Reader {
    Reader::Checked(json5_value_check, parsing!(tabnas_json5::make()))
}

/// An engine error as transduce's parser source reports one: the input's,
/// with the engine's code and position, and a cancel, which no abort of
/// this command's asks for, a guard of the grammar's own. (The engine's
/// error is the one every grammar re-exports under a name of its own.)
fn engine_failure(error: &tabnas_json::JsonError) -> Fail {
    let mut fail = Fail::from_tabnas(error);
    if error.code == "cancel" {
        fail.message = format!(
            "the grammar stopped the parse with a guard of its own ({}: {}); a grammar \
             may refuse nesting or size below this crate's Limits",
            error.code,
            error.detail.trim_end()
        );
    }
    fail
}

/// An expression as expr's API reads one for its shared fixtures, the tree
/// its parts declare: each operation a list whose first element is the
/// operator's source text (`parse_simplified`). The parser's own value
/// holds each operation as a handle into the parse's arena, which only the
/// package's API reads.
fn read_expr(text: &str) -> Result<Datum, Fail> {
    tabnas_expr::parse_simplified(&tabnas_expr::make(), text)
        .map(|tree| Datum::from_tabnas(&tree))
        .map_err(|error| engine_failure(&error))
}

/// A `.proto` document as proto's API reads one, the tree its parts
/// declare: the FileDescriptorProto the canonical parse returns, every
/// member named and ordered as its object has them (`parse_value`). Its
/// parse makes the check the parser does not, before the engine runs: a
/// document nesting deeper than its cap (`tabnas_proto::MAX_NESTING_DEPTH`)
/// is refused before the engine builds a tree that deep, whose drop could
/// abort the process. The parser's own value is the grammar's syntax tree,
/// which only the package's API reads.
fn read_proto(text: &str) -> Result<Datum, Fail> {
    tabnas_proto::parse_value(text, None)
        .map(|tree| Datum::from_tabnas(&tree))
        .map_err(|error| match error {
            tabnas_proto::ProtoError::Parse(error) => engine_failure(&error),
            refused => Fail::input(refused.to_string()),
        })
}

/// A C translation unit as c's API reads one, the tree its parts declare
/// (`parse`, which realizes the parse into plain values). The parser's own
/// value is a handle into the parse's per-thread arena, which only the
/// package's API reads.
fn read_c(text: &str) -> Result<Datum, Fail> {
    tabnas_c::parse(text)
        .map(|tree| Datum::from_tabnas(&tree))
        .map_err(|error| engine_failure(&error))
}

/// A grammar spec's strict JSON text, which a notation's compiler writes, as
/// the tree it holds, read with the JSON grammar.
fn spec_tree(json: &str) -> Result<Datum, Fail> {
    tabnas_json::parse(json)
        .map(|spec| Datum::from_tabnas(&spec))
        .map_err(|error| engine_failure(&error))
}

/// An ABNF document as abnf's API reads one for a host, the tree its parts
/// declare: the pure-data grammar spec its compiler writes (`abnf_compile`,
/// recognition off, so that the tree builders stay, as strict JSON), read
/// as JSON. The grammar spec is the schema grammar-spec, which the EBNF and
/// GBNF compilers emit too.
fn read_abnf(text: &str) -> Result<Datum, Fail> {
    let options = tabnas_abnf::AbnfCompileOptions {
        start: None,
        tag: None,
        recognition: false,
        strict: true,
        indent: None,
    };
    let compiled = tabnas_abnf::abnf_compile(text, &options)
        .map_err(|error| Fail::input(error.to_string()))?;
    spec_tree(&compiled)
}

/// A notation's conversion, converted with the builtins on, as the
/// shared compiler writes it for a host: the pure-data grammar spec, with
/// its tree builders (`compile_spec` with recognition off, which is
/// `to_jsonic` over `to_pure_spec`), as strict JSON, read as JSON. The
/// compiler is tabnas-bnf, whose serializers tabnas-abnf re-exports, so
/// reading a notation takes no package beyond the notations'.
fn compiled_tree(spec: &tabnas_abnf::GrammarSpec) -> Result<Datum, Fail> {
    let pure = tabnas_abnf::to_pure_spec(spec).map_err(|error| Fail::input(error.to_string()))?;
    let json = tabnas_abnf::to_jsonic(
        &pure,
        tabnas_abnf::JsonicOptions {
            strict: true,
            indent: None,
        },
    );
    spec_tree(&json)
}

/// An EBNF document as ebnf's API reads one for a host, the tree its parts
/// declare: the grammar spec the shared compiler writes of `ebnf_convert`
/// with the builtins on ([`compiled_tree`]).
fn read_ebnf(text: &str) -> Result<Datum, Fail> {
    let convert = tabnas_ebnf::EbnfConvertOptions {
        builtins: true,
        ..tabnas_ebnf::EbnfConvertOptions::default()
    };
    let spec = tabnas_ebnf::ebnf_convert(text, Some(&convert))
        .map_err(|error| Fail::input(error.to_string()))?;
    compiled_tree(&spec)
}

/// A GBNF document as gbnf's API reads one for a host, the tree its parts
/// declare: the grammar spec the shared compiler writes of `gbnf_convert`
/// with the builtins on, and the notation's own defaults otherwise (the
/// start `root`, eager classes where they are unambiguous)
/// ([`compiled_tree`]).
fn read_gbnf(text: &str) -> Result<Datum, Fail> {
    let convert = tabnas_gbnf::GbnfConvertOptions::new(tabnas_gbnf::ConvertOptions {
        builtins: true,
        ..tabnas_gbnf::ConvertOptions::default()
    });
    let spec = tabnas_gbnf::gbnf_convert(text, Some(&convert))
        .map_err(|error| Fail::input(error.to_string()))?;
    compiled_tree(&spec)
}

/// Every grammar package this command carries, with its reader.
fn packages() -> Vec<(Option<Described>, Reader)> {
    vec![
        (
            descriptor!("tabnas-abnf", tabnas_abnf, embed),
            Reader::Tree(read_abnf),
        ),
        (
            descriptor!("tabnas-c", tabnas_c, embed),
            Reader::Tree(read_c),
        ),
        (descriptor!("tabnas-chess", tabnas_chess, embed), read_pgn()),
        (
            descriptor!("tabnas-css", tabnas_css, embed),
            reader!(tabnas_css),
        ),
        (
            descriptor!("tabnas-csv", tabnas_csv, embed),
            reader!(tabnas_csv),
        ),
        (
            descriptor!("tabnas-ebnf", tabnas_ebnf, embed),
            Reader::Tree(read_ebnf),
        ),
        (
            descriptor!("tabnas-expr", tabnas_expr, embed),
            Reader::Tree(read_expr),
        ),
        (
            descriptor!("tabnas-feed", tabnas_feed, embed),
            reader!(tabnas_feed),
        ),
        (
            descriptor!("tabnas-gbnf", tabnas_gbnf, embed),
            Reader::Tree(read_gbnf),
        ),
        (
            descriptor!("tabnas-ini", tabnas_ini, embed),
            reader!(tabnas_ini),
        ),
        (
            descriptor!("tabnas-json", tabnas_json, embed),
            reader!(tabnas_json),
        ),
        (
            descriptor!("tabnas-json5", tabnas_json5, embed),
            read_json5(),
        ),
        (
            descriptor!("tabnas-jsonc", tabnas_jsonc, embed),
            reader!(tabnas_jsonc),
        ),
        (
            descriptor!("tabnas-jsonic", tabnas_jsonic, embed),
            reader!(tabnas_jsonic),
        ),
        (
            descriptor!("tabnas-jsonl", tabnas_jsonl, embed),
            reader!(tabnas_jsonl),
        ),
        (
            descriptor!("tabnas-markdown", tabnas_markdown, embed),
            reader!(tabnas_markdown),
        ),
        (
            descriptor!("tabnas-proto", tabnas_proto, embed),
            Reader::Tree(read_proto),
        ),
        (
            descriptor!("tabnas-semver", tabnas_semver, embed),
            reader!(tabnas_semver),
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
            .filter_map(|(described, reader)| {
                let Described {
                    descriptor,
                    transduce,
                } = described?;
                let part = Part::from_descriptor(&descriptor)?;
                // A transducer the manifest and the package disagree on is
                // none here; the translation-parts test holds the two
                // together.
                let transducer = Part::transducer(&descriptor, transduce.as_ref()).flatten();
                Some(Format {
                    part,
                    reader,
                    transducer,
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
    names_of(formats().iter().map(Format::id))
}

/// The ids of the formats whose documents are parsed, for a message, as
/// [`names`] lists every format: what `run --grammar` takes.
pub fn parser_names() -> String {
    names_of(formats().iter().filter(|f| f.parses()).map(Format::id))
}

fn names_of<'a>(ids: impl Iterator<Item = &'a str>) -> String {
    let ids: Vec<&str> = ids.collect();
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

/// What a request compiles to: the composition over the source's events,
/// the route every format has, and, where the source ships a transducer
/// part and the whole document is asked, the composition over its parser's
/// rule passes, which [`run_compiled`] runs first.
pub struct Compiled {
    pub composition: Composition,
    pub program: Program,
    parsed: Option<(Composition, Program)>,
}

/// The composition a request makes, and the program it compiles to.
pub fn compile(request: &Request<'_>) -> Result<Compiled, Fail> {
    let routers = Arc::new(tabnas_transduce::routers());
    let renderers = Arc::new(tabnas_render::renderers());
    // The transducer the route reads the document through, where the
    // source ships one and the whole document is asked: a path selects a
    // value below the root, which only the document's value has.
    let transducer = match request.path {
        None => request.from.transducer(),
        Some(_) => None,
    };
    let (composition, program, parsed) = match request.program {
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
            let program = composition.compile(
                Some(Source::new(file, text)),
                routers.clone(),
                renderers.clone(),
            )?;
            let parsed = match transducer {
                Some(alc) => {
                    let composition = compose_program_parsed(
                        output,
                        alc,
                        &request.to.part,
                        &request.options,
                        "translate",
                    )?;
                    let program =
                        composition.compile(Some(Source::new(file, text)), routers, renderers)?;
                    Some((composition, program))
                }
                None => None,
            };
            (composition, program, parsed)
        }
        None => {
            let source = match request.path {
                Some(_) => None,
                None => Some(&request.from.part),
            };
            let composition = compose(source, &request.to.part, &request.options, "translate")?;
            let program = composition.compile(None, routers.clone(), renderers.clone())?;
            let parsed = match transducer {
                Some(alc) => {
                    let composition = compose_parsed(
                        &request.from.part,
                        alc,
                        &request.to.part,
                        &request.options,
                        "translate",
                    )?;
                    let program = composition.compile(None, routers, renderers)?;
                    Some((composition, program))
                }
                None => None,
            };
            (composition, program, parsed)
        }
    };
    Ok(Compiled {
        composition,
        program,
        parsed,
    })
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
    out: Box<dyn Write + Send>,
    metrics: Arc<Metrics>,
) -> Result<(), Fail> {
    run_compiled(request, &compile(request)?, input, out, metrics)
}

/// [`run`], with the request's composition compiled already
/// ([`compile`]), so that one compiled composition serves many inputs.
pub fn run_compiled(
    request: &Request<'_>,
    compiled: &Compiled,
    input: &str,
    mut out: Box<dyn Write + Send>,
    metrics: Arc<Metrics>,
) -> Result<(), Fail> {
    let Compiled {
        composition,
        program,
        parsed,
    } = compiled;
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
                source(request.from, input, mode, &request.limits, metrics, sink).0?;
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
    // The transducer's route first, where the source ships a part and the
    // whole document is asked: its rule passes through the part into the
    // program, streamed whatever the grammar. A document whose events are
    // not what the grammar's value holds (a repeated member, streamed
    // twice, which the composition's tree-contract refuses) is read again
    // whole below, as the incremental source's is: the grammar's own value
    // is the document.
    let transduced = match parsed {
        Some((_, program)) => {
            let own = Metrics::new();
            match attempt_parsed(request, program, input, own.clone()) {
                Err(fail) if unfollowed(&fail) => None,
                outcome => {
                    let bytes = outcome?;
                    absorb(&metrics, &own);
                    Some(bytes)
                }
            }
        }
        None => None,
    };
    let incremental = request.path.is_none() && capability::incremental(request.from.id());
    let bytes = if let Some(bytes) = transduced {
        bytes
    } else if incremental {
        let mode = SourceMode::Incremental {
            prune: program
                .row_selector()
                .map_or(Prune::Never, |s| Prune::Under(s.clone())),
        };
        // The incremental attempt counts into metrics of its own, which
        // the caller's take on once it has succeeded: a run given up part
        // way counts nothing.
        let own = Metrics::new();
        match attempt(mode, own.clone()) {
            Err(fail) if unfollowed(&fail) => attempt(SourceMode::Materialize, metrics)?,
            outcome => {
                let bytes = outcome?;
                absorb(&metrics, &own);
                bytes
            }
        }
    } else {
        attempt(SourceMode::Materialize, metrics)?
    };
    let mut written = 0;
    write_out(&mut out, &bytes, &mut written).map_err(|e| {
        let fail = Fail::new(
            Code::OutputFailed,
            format!("the output could not be written: {e}"),
        );
        // Bytes the writer took before it failed have left.
        if written > 0 {
            fail.committed()
        } else {
            fail
        }
    })
}

/// One run of a request over its source's parser's rule passes, through
/// the composition over the source's transducer part: what it wrote, held
/// until the run has succeeded, as [`run_compiled`]'s other attempts hold
/// it.
fn attempt_parsed(
    request: &Request<'_>,
    program: &Program,
    input: &str,
    metrics: Arc<Metrics>,
) -> Result<Vec<u8>, Fail> {
    let held = Held::default();
    let sink = program.parser_sink(
        Box::new(held.clone()),
        None,
        &request.limits,
        metrics.clone(),
    )?;
    request
        .from
        .passes(input)?
        .limits(request.limits.clone())
        .metrics(metrics)
        .run_boxed(sink)
        .0?;
    let bytes = std::mem::take(
        &mut *held
            .0
            .lock()
            .map_err(|_| Fail::new(Code::OutputFailed, "the held output's lock was poisoned"))?,
    );
    Ok(bytes)
}

/// Write `bytes` to `out` and flush it, counting in `written` the bytes
/// the writer took.
fn write_out(out: &mut dyn Write, bytes: &[u8], written: &mut usize) -> std::io::Result<()> {
    while *written < bytes.len() {
        match out.write(&bytes[*written..]) {
            Ok(0) => return Err(std::io::ErrorKind::WriteZero.into()),
            Ok(n) => *written += n,
            Err(e) if e.kind() == std::io::ErrorKind::Interrupted => {}
            Err(e) => return Err(e),
        }
    }
    out.flush()
}

/// What an attempt counted, added to the caller's metrics: the counts
/// summed and the high-water marks raised, so that the metrics a run hands
/// back are those of the attempt whose output was written.
fn absorb(into: &Metrics, from: &Metrics) {
    // Every field by name, so that a field Metrics gains is not left out.
    let Metrics {
        events,
        keys,
        scalars,
        rows,
        captured_bytes,
        captured_bytes_high,
        retained_bytes_high,
        output_bytes,
    } = from;
    for (to, n) in [
        (&into.events, events),
        (&into.keys, keys),
        (&into.scalars, scalars),
        (&into.rows, rows),
        (&into.captured_bytes, captured_bytes),
        (&into.output_bytes, output_bytes),
    ] {
        Metrics::add(to, Metrics::get(n));
    }
    Metrics::raise(&into.captured_bytes_high, Metrics::get(captured_bytes_high));
    Metrics::raise(&into.retained_bytes_high, Metrics::get(retained_bytes_high));
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

/// A document's events as a format reads it, into `sink`, which comes back
/// with the outcome: its parser's, through transduce's parser source in
/// `mode` (after the check its package's parse makes first, where it makes
/// one), or the tree its package's API reads, read whole whatever the mode
/// and walked under the same limits and counts.
fn source<S: Sink + Send + 'static>(
    format: &Format,
    input: &str,
    mode: SourceMode,
    limits: &Limits,
    metrics: Arc<Metrics>,
    sink: S,
) -> (Result<Flow, Fail>, S) {
    match format.reader {
        Reader::Parser(parsing) => (parsing.events)(input)
            .grammar(format.id())
            .mode(mode)
            .limits(limits.clone())
            .metrics(metrics)
            .run_owned(sink),
        Reader::Checked(check, parsing) => match check(input) {
            Err(refused) => (Err(refused), sink),
            Ok(()) => (parsing.events)(input)
                .grammar(format.id())
                .mode(mode)
                .limits(limits.clone())
                .metrics(metrics)
                .run_owned(sink),
        },
        Reader::Tree(read) => {
            let mut guarded = Guarded::new(sink, limits, AbortFlag::new(), metrics);
            let outcome = read(input).and_then(|tree| match walk_datum(&tree, &mut guarded)? {
                Flow::Continue => guarded.event(JsonEvent::End),
                Flow::Stop => Ok(Flow::Stop),
            });
            (outcome, guarded.into_inner())
        }
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
    let (outcome, mut collect) = source(format, input, mode, limits, Metrics::new(), collect);
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
/// schema, why its documents are read whole where it says (`whole`, null
/// where it does not), its parts' entries, and its loss sentences.
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
            let fields: [(&str, Datum); 11] = [
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
                ("whole", p.whole.as_deref().map_or(Datum::Null, text)),
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
                    "transduce",
                    f.transducer
                        .as_ref()
                        .map_or(Datum::Null, |a| text(&a.entry)),
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

#[cfg(test)]
mod tests {
    use super::*;

    /// The json transducer, as its package's part spells it: the library's
    /// `transduce-tree` over the json grammar's rules.
    const JSON_TRANSDUCE: &str = "def json-transduce [input]\n  transduce-tree\n    record\n      entry :map (record (entry :container :object))\n      entry :list (record (entry :container :array))\n      entry :pair (record (entry :key 0))\n      entry :val (record (entry :scalar 0))\n    input\n";

    /// A transducer that writes `{\"ran\":true}` whatever the document:
    /// what it writes says which route ran.
    const MARKING_TRANSDUCE: &str = "def json-transduce [input]\n  as-events\n    scan-emit\n      []\n      fn [s pass] (transition s [])\n      fn [s] [object-start (key \"ran\") (scalar true) object-end]\n      parser-events input\n";

    /// json, read through a transducer part given beside its package's
    /// parts, as a package that ships one is read.
    fn transduced_json(text: &str) -> Format {
        let json = format("json").expect("json is a format");
        Format {
            part: json.part.clone(),
            reader: json.reader,
            transducer: Some(Alc {
                file: "tabnas-json/alchemy/transduce.alc".to_string(),
                entry: "json-transduce".to_string(),
                text: text.to_string(),
            }),
        }
    }

    fn translated(
        from: &Format,
        to: &str,
        path: Option<&str>,
        program: Option<(&str, &str)>,
        input: &str,
    ) -> Result<String, Fail> {
        let limits = Limits::default();
        let request = Request {
            from,
            to: format(to).expect("a format"),
            path: path.map(|p| parse_path(p, &limits).expect("a path")),
            options: Options::default(),
            program,
            limits,
        };
        let held = Held::default();
        run(&request, input, Box::new(held.clone()), Metrics::new())?;
        let bytes = held.0.lock().unwrap().clone();
        Ok(String::from_utf8(bytes).expect("the output is UTF-8"))
    }

    /// A format whose package ships a transducer part is read through it,
    /// its rule passes streamed into the composition over the part, and
    /// one without is read as before: the two routes write the same bytes
    /// for the same document, a number's lexeme kept, into a tree target,
    /// a records target, and with a program over the input; the part's
    /// own output shows the route ran. A path keeps the document's value,
    /// the one route a path has. A repeated member, which the part
    /// streams twice and the composition's tree-contract refuses, is read
    /// again whole, as the incremental source's is.
    #[test]
    fn a_transducer_part_reads_the_format_and_the_walk_stands_in_where_it_refuses() {
        let plain = format("json").expect("json is a format");
        let through = transduced_json(JSON_TRANSDUCE);
        let marking = transduced_json(MARKING_TRANSDUCE);
        assert!(plain.transducer().is_none());
        assert_eq!(
            through.transducer().map(|alc| alc.entry.as_str()),
            Some("json-transduce")
        );
        let tree = r#"{"a":[1,2.50,"x",{"b":null}],"c":{},"d":[],"e":1e21,"f":true}"#;
        let records = r#"[{"a":1,"b":"x"},{"a":2,"b":"y"}]"#;
        let echo = Some(("echo.alc", "def export [input] (events input)\n"));
        for (to, doc, program) in [
            ("json", tree, None),
            ("yaml", tree, None),
            ("csv", records, None),
            ("json", tree, echo),
            ("csv", records, echo),
        ] {
            let expected =
                translated(plain, to, None, program, doc).expect("the plain route writes");
            let got = translated(&through, to, None, program, doc)
                .expect("the transducer's route writes");
            assert_eq!(got, expected, "{to} from {doc} with {program:?}");
            let marked = translated(&marking, to, None, program, doc);
            assert_ne!(
                marked.as_deref().ok(),
                Some(expected.as_str()),
                "{to} from {doc} with {program:?}: the part's route ran"
            );
        }
        assert_eq!(
            translated(&marking, "json", None, None, tree).unwrap(),
            "{\"ran\":true}\n"
        );
        // A path selects in the document's value, read whole, as it did:
        // the materialized value spells a number as the engine does.
        assert_eq!(
            translated(&marking, "json", Some("[\"a\",1]"), None, tree).unwrap(),
            "2.5\n",
            "a path keeps the document's value"
        );
        let repeated = r#"{"a":1,"a":2}"#;
        assert_eq!(
            translated(&through, "json", None, None, repeated).unwrap(),
            translated(plain, "json", None, None, repeated).unwrap()
        );
        assert_eq!(
            translated(&through, "json", None, None, repeated).unwrap(),
            "{\"a\":2}\n"
        );
        let refused = translated(&through, "json", None, None, "[1,").unwrap_err();
        assert_eq!(refused.code, Code::InputInvalid);
    }

    /// Every format with a parser answers its rule passes; one read as a
    /// tree has none and refuses; json5's check runs first.
    #[test]
    fn a_format_with_a_parser_gives_its_rule_passes() {
        let parsed: Vec<&str> = formats()
            .iter()
            .filter(|f| f.parses())
            .map(Format::id)
            .collect();
        assert_eq!(
            parsed,
            [
                "css", "csv", "feed", "ini", "json", "json5", "jsonc", "jsonic", "jsonl",
                "markdown", "pgn", "semver", "toml", "xml", "yaml", "zon"
            ]
        );
        assert_eq!(
            parser_names(),
            "css, csv, feed, ini, json, json5, jsonc, jsonic, jsonl, markdown, pgn, semver, toml, \
             xml, yaml or zon"
        );
        let abnf = format("abnf").expect("abnf is a format");
        assert!(!abnf.parses());
        let refused = abnf
            .passes("x = \"y\"")
            .err()
            .expect("a tree has no passes");
        assert_eq!(refused.code, Code::InputInvalid);
        assert!(
            refused.message.starts_with("abnf is read as a tree"),
            "{refused}"
        );
        let json5 = format("json5").expect("json5 is a format");
        let empty = json5
            .passes("// only a comment")
            .err()
            .expect("the check refuses");
        assert_eq!(empty.code, Code::InputInvalid);
        let (outcome, passes) = json5
            .passes("{a: 1}")
            .expect("the check passes")
            .run_owned(Vec::new());
        outcome.expect("the passes stream");
        assert!(!passes.is_empty());
    }
}
