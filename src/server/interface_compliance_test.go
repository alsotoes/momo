package server

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestServerNoProtocolImports verifies that server.go does not import
// protocol implementations directly. It should only use the Communicator
// interface from the transport package.
func TestServerNoProtocolImports(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("failed to parse server.go: %v", err)
	}

	forbiddenImports := []string{
		"github.com/alsotoes/momo/src/transport/momo_tcp",
		"github.com/alsotoes/momo/src/transport/momo_quic",
		"github.com/alsotoes/momo/src/transport/s3_communicator",
	}

	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		for _, forbidden := range forbiddenImports {
			if importPath == forbidden {
				t.Errorf("server.go imports forbidden protocol implementation: %s", forbidden)
			}
		}
	}
}

// TestServerOnlyUsesCommunicatorInterface verifies that server.go only
// calls methods on the Communicator interface (and optional capability
// interfaces) and doesn't perform type assertions on concrete protocol types.
func TestServerOnlyUsesCommunicatorInterface(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "server.go", nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("failed to parse server.go: %v", err)
	}

	// Check for type assertions on concrete protocol types
	ast.Inspect(f, func(n ast.Node) bool {
		if ta, ok := n.(*ast.TypeAssertExpr); ok {
			if ident, ok := ta.Type.(*ast.Ident); ok {
				switch ident.Name {
				case "MomoTCPCommunicator", "MomoQUICCommunicator", "S3Communicator":
					t.Errorf("server.go performs type assertion on concrete protocol type: %s", ident.Name)
				}
			}
		}
		return true
	})
}
