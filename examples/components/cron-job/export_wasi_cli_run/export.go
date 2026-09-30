// Package export_wasi_cli_run is the export trampoline for
// `wasi:cli/run@0.2.8`. It is handwritten, not generated.
//
// The generated glue in ../wit_exports carries the `//go:wasmexport` function
// the host actually calls, and that function forwards to Run here. The glue
// names this package by import path, so the path and the package name are
// fixed by the generator; what lives inside is up to you. Keeping the seam
// in a package of its own is what lets wit_exports be regenerated wholesale
// without touching the implementation: main.go assigns Exports.Run during
// init, and the two never have to know about each other.
package export_wasi_cli_run

import (
	witTypes "go.bytecodealliance.org/pkg/wit/types"
)

// Exports holds the implementation. main.go registers during init, which
// runs before the host can call the export.
var Exports struct {
	Run func() witTypes.Result[witTypes.Unit, witTypes.Unit]
}

// Run is what the generated glue calls when the host invokes
// `wasi:cli/run#run`.
func Run() witTypes.Result[witTypes.Unit, witTypes.Unit] {
	if Exports.Run == nil {
		// Trapping is the honest outcome for a component that never wired
		// itself up: the host logs the trap instead of reporting a clean exit.
		panic("wasi:cli/run#run invoked before an implementation was registered")
	}
	return Exports.Run()
}
