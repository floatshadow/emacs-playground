package tsgen

import (
	"go/importer"
	"go/token"
	"go/types"
)

// importerFor returns a source importer for the standard library imports of
// the protocol package.
func importerFor(fset *token.FileSet) types.Importer {
	return importer.ForCompiler(fset, "source", nil)
}
