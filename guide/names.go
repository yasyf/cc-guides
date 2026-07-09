package guide

// Name, key, and argument-value validators. These are the single source of truth
// for the character sets a fragment name, an argument key, and an argument value
// may use — shared by the v1 directive parser (parse.go) and the v3 layout schema
// (the layout package). Argument values are substituted verbatim into generated
// artifacts, so ValidArgValue is deliberately strict: bare tokens only, nothing
// that could leak a quote, space, or shell metacharacter into the rendered shell.

// ValidName reports whether s is a legal fragment name or source alias
// (^[a-z0-9][a-z0-9-]*$).
func ValidName(s string) bool { return nameRe.MatchString(s) }

// ValidArgKey reports whether s is a legal argument key (^[a-z][a-z0-9-]*$).
func ValidArgKey(s string) bool { return keyRe.MatchString(s) }

// ValidArgValue reports whether s is a legal argument value
// (^[A-Za-z0-9._/@:+-]+$) — letters, digits, and ._/@:+-, covering binary names,
// owner/repo, brew taps, plugin names, and versions.
func ValidArgValue(s string) bool { return argValueRe.MatchString(s) }
