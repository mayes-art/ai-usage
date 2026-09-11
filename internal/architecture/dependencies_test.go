// Package architecture verifies the dependency direction that keeps the domain,
// application service and MVC controller replaceable and independently testable.
package architecture

import (
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLayerDependencies(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]map[string]bool{
		"model":      {},
		"service":    {"aiusage/internal/model": true},
		"controller": {"aiusage/internal/model": true, "aiusage/internal/view": true},
		"view":       {"aiusage/internal/model": true},
	}
	for layer, internalImports := range allowed {
		t.Run(layer, func(t *testing.T) {
			files := 0
			err := filepath.WalkDir(filepath.Join(root, "internal", layer), func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				files++
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imported := range file.Imports {
					name, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						return err
					}
					if internalImports[name] {
						continue
					}
					pkg, err := build.Default.Import(name, root, build.FindOnly)
					if err != nil || !pkg.Goroot {
						t.Errorf("%s imports %q: %s may depend only on its declared inner layers and the standard library", path, name, layer)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if files == 0 {
				t.Fatal("no production source found for layer")
			}
		})
	}
}
