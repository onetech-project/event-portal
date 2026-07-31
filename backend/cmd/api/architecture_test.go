package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/manjo/ticketing/backend"

// domains is the closed set Constitution Principle I allows.
var domains = []string{"admin", "event", "order", "payment", "ticket", "notification"}

// These rules are the load-bearing part of the modular monolith: if they hold, a
// domain can be lifted into its own service later without untangling an import
// graph. Checking them here — in the composition root, the one package allowed to
// see every domain — keeps them from eroding silently.

type goFile struct {
	path    string
	domain  string
	name    string
	imports []string
	isTest  bool
}

func loadInternalFiles(t *testing.T) []goFile {
	t.Helper()

	root, err := filepath.Abs("../../internal")
	require.NoError(t, err)

	var files []goFile
	fset := token.NewFileSet()

	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		parsed, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}

		imports := make([]string, 0, len(parsed.Imports))
		for _, spec := range parsed.Imports {
			imports = append(imports, strings.Trim(spec.Path.Value, `"`))
		}

		files = append(files, goFile{
			path:    rel,
			domain:  strings.Split(rel, string(filepath.Separator))[0],
			name:    filepath.Base(rel),
			imports: imports,
			isTest:  strings.HasSuffix(rel, "_test.go"),
		})
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)

	return files
}

// Constitution Principle II: a domain must not import another domain — not its
// repository, and not any other part of it. Cross-domain needs go through an
// interface the consumer declares, which this composition root satisfies.
func TestNoDomainImportsAnotherDomain(t *testing.T) {
	for _, file := range loadInternalFiles(t) {
		if file.isTest {
			// Test files legitimately wire domains together to exercise the same
			// adapters this package wires in production.
			continue
		}

		for _, imported := range file.imports {
			for _, other := range domains {
				if other == file.domain {
					continue
				}
				assert.NotEqual(t, modulePath+"/internal/"+other, imported,
					"%s imports the %s domain; use a consumer-declared interface instead", file.path, other)
				assert.False(t, strings.HasPrefix(imported, modulePath+"/internal/"+other+"/"),
					"%s imports %s from the %s domain", file.path, imported, other)
			}
		}
	}
}

// Constitution Principle III: sqlc-generated structs must not reach the wire. The
// generated package is confined to each domain's repository; DTOs and handlers
// must not see it.
func TestGeneratedStructsStayOutOfTheTransportLayer(t *testing.T) {
	for _, file := range loadInternalFiles(t) {
		if file.isTest || !isTransportFile(file.name) {
			continue
		}

		for _, imported := range file.imports {
			assert.False(t, strings.HasSuffix(imported, "sql"),
				"%s imports the generated package %s; map to a DTO in the repository instead",
				file.path, imported)
		}
	}
}

// Every domain owns the exact JSON shape it returns.
func TestEveryDomainDefinesItsOwnDTOs(t *testing.T) {
	withDTO := map[string]bool{}
	for _, file := range loadInternalFiles(t) {
		if file.name == "dto.go" {
			withDTO[file.domain] = true
		}
	}

	for _, domain := range domains {
		assert.True(t, withDTO[domain], "domain %q has no dto.go", domain)
	}
}

// Constitution Principle I: business logic lives under internal/<domain>/ and the
// domain list is closed.
func TestOnlyDeclaredDomainsExist(t *testing.T) {
	allowed := map[string]bool{
		// testsupport is a test-only fixture package, not a domain.
		"testsupport": true,
	}
	for _, domain := range domains {
		allowed[domain] = true
	}

	for _, file := range loadInternalFiles(t) {
		assert.True(t, allowed[file.domain],
			"%s lives outside the declared domains", file.path)
	}
}

func isTransportFile(name string) bool {
	switch name {
	case "dto.go", "admin_dto.go", "handler.go", "admin_handler.go":
		return true
	default:
		return false
	}
}
