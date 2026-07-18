package guide

import "errors"

// Sentinel error taxonomy. Every composition/fragment failure wraps one of these
// so callers (the CLI) can map them to exit codes without string matching. All of
// them denote invalid input (CLI exit 2); drift (STALE/MISSING, exit 1) is
// signalled by the commands, not by these sentinels.
var (
	// ErrCRLF is returned when input carries CR bytes. CRLF is a hard error and is
	// never silently normalized.
	ErrCRLF = errors.New("CRLF line endings are not supported (convert to LF)")

	// ErrUnknownFragment is an import naming a fragment no source knows.
	ErrUnknownFragment = errors.New("unknown fragment")

	// ErrKindMismatch is an import pulling a fragment of the wrong kind for the
	// artifact (e.g. a shell fragment into a markdown artifact).
	ErrKindMismatch = errors.New("fragment kind mismatch")

	// ErrShebangNotFirst is a composition piece other than the first that begins
	// with `#!`. Only the first piece of a .sh artifact may carry a shebang.
	ErrShebangNotFirst = errors.New("only the first piece of a shell artifact may begin with a shebang")

	// ErrTokenNoArg is a `{{token}}` in a fragment body with no matching entry
	// argument.
	ErrTokenNoArg = errors.New("fragment token has no matching entry argument")

	// ErrArgUnused is a layout entry argument that the fragment body never consumes.
	ErrArgUnused = errors.New("entry argument is not used by the fragment")

	// ErrUnknownExt is an unsupported artifact/fragment extension.
	ErrUnknownExt = errors.New("unsupported extension")

	// ErrHandwrittenOverwrite is a render refusing to clobber a handwritten file
	// cc-guides does not manage (no marker, not in the lock).
	ErrHandwrittenOverwrite = errors.New("refusing to overwrite a handwritten file cc-guides does not manage")

	// ErrJSONParse is a JSON fragment that is not well-formed or carries trailing data.
	ErrJSONParse = errors.New("invalid JSON fragment")

	// ErrJSONNotObject is a JSON fragment whose root value is not an object.
	ErrJSONNotObject = errors.New("JSON fragment root must be an object")

	// ErrYAMLParse is a YAML fragment that is not well-formed YAML.
	ErrYAMLParse = errors.New("invalid YAML fragment")

	// ErrShellParse is a shell fragment that is not well-formed under the grammar.
	ErrShellParse = errors.New("invalid shell fragment")

	// ErrTOMLParse is a TOML fragment that is not well-formed under the grammar.
	ErrTOMLParse = errors.New("invalid TOML fragment")

	// ErrTOMLDecode is a TOML fragment (or a composed TOML artifact) that the strict
	// decoder rejects — most usefully a table defined twice, which the grammar accepts
	// but TOML semantics forbid.
	ErrTOMLDecode = errors.New("invalid TOML")
)
