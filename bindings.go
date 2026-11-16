package main

import (
	"embed"

	"ofam.dev/ochazuke"
)

// bindings.json holds key rows as data, loaded into the default layer
// (below the user's bindings.json) after every verb is registered.

//go:embed bindings.json
var bindingsFS embed.FS

// loadDefaultBindings panics on a row naming an unknown verb: the file is
// embedded, so that is a build error.
func loadDefaultBindings(r *ochazuke.Runtime) {
	if err := r.LoadDefaultBindingsFS(bindingsFS, "bindings.json"); err != nil {
		panic(err)
	}
}
