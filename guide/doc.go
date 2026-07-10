// Package guide is a generic, kind-aware composer for artifacts built from local
// prose fragments and imported shared fragments. It knows nothing about the
// specific fragments it composes: callers resolve imports through a source.Importer
// and pass the resolved bodies as Pieces. The pipeline is Compose (or ComposeJSON)
// over the ordered pieces → AddMarker, with a sentinel error taxonomy the CLI maps
// to exit codes.
package guide
