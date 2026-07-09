package guide

import "errors"

// Sentinel error taxonomy. Every parse/render/fragment failure wraps one of
// these so callers (the CLI) can map them to exit codes without string matching.
// All of them denote invalid input (CLI exit 2); drift (STALE/MISSING/init
// mismatch, exit 1) is signalled by the commands, not by these sentinels.
var (
	// ErrCRLF is returned when input carries CR bytes. CRLF is a hard error and is
	// never silently normalized.
	ErrCRLF = errors.New("CRLF line endings are not supported (convert to LF)")

	// ErrMalformedDirective is a column-0 `{{>` line that is not a well-formed
	// directive.
	ErrMalformedDirective = errors.New("malformed include directive")

	// ErrLegacyPathDirective is a legacy path-form directive like
	// `{{> _partials/x.md}}`; bare names are required.
	ErrLegacyPathDirective = errors.New("legacy path-form include directive")

	// ErrBadName is a directive name that violates ^[a-z0-9][a-z0-9-]*$.
	ErrBadName = errors.New("invalid fragment name")

	// ErrBadArg is a malformed key=value directive argument.
	ErrBadArg = errors.New("invalid directive argument")

	// ErrDuplicateArg is the same key passed twice in one directive.
	ErrDuplicateArg = errors.New("duplicate directive argument")

	// ErrBadArgValue is a directive argument value outside the allowed character
	// set. Values are substituted verbatim into generated artifacts, so they are
	// restricted to bare tokens — letters, digits, and ._/@:+- — which covers
	// binary names, owner/repo, brew taps, plugin names, and versions. Quotes,
	// spaces, commas, and shell metacharacters (e.g. `$(…)`) are rejected to
	// prevent leaking them, or an injection, into the rendered shell.
	ErrBadArgValue = errors.New("invalid directive value: allowed characters are letters, digits, and ._/@:+-")

	// ErrUnknownFragment is a directive naming a fragment no resolver knows.
	ErrUnknownFragment = errors.New("unknown fragment")

	// ErrKindMismatch is a directive including a fragment of the wrong kind for
	// the artifact (e.g. a shell fragment into a markdown artifact).
	ErrKindMismatch = errors.New("fragment kind mismatch")

	// ErrNestedInclude is a directive-shaped line inside a resolved fragment body;
	// expansion is one level only.
	ErrNestedInclude = errors.New("nested include is not allowed (expansion is one level)")

	// ErrShebangNotFirst is a composition piece other than the first that begins
	// with `#!`. Only the first piece of a .sh artifact may carry a shebang.
	ErrShebangNotFirst = errors.New("only the first piece of a shell artifact may begin with a shebang")

	// ErrTokenNoArg is a `{{token}}` in a fragment body with no matching directive
	// argument.
	ErrTokenNoArg = errors.New("fragment token has no matching directive argument")

	// ErrArgUnused is a directive argument that the fragment body never consumes.
	ErrArgUnused = errors.New("directive argument is not used by the fragment")

	// ErrUnknownExt is an unsupported artifact/fragment extension.
	ErrUnknownExt = errors.New("unsupported extension")

	// ErrBannerlessOverwrite is a render refusing to clobber a handwritten file
	// that carries no cc-guides banner.
	ErrBannerlessOverwrite = errors.New("refusing to overwrite a file without a cc-guides banner")
)
