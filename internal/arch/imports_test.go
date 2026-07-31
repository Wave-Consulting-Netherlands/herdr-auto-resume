package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/Wave-Consulting-Netherlands/herdr-auto-resume"

func TestCoreImportHygiene(t *testing.T) {
	for _, dir := range []string{"../coordinator", "../detection", "../runtime", "../store", "../jobs"} {
		t.Run(dir, func(t *testing.T) {
			root, err := filepath.Abs(dir)
			if err != nil {
				t.Fatalf("resolve %s: %v", dir, err)
			}
			err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() && path != root {
					return filepath.SkipDir
				}
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					return nil
				}

				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imported := range file.Imports {
					importPath, err := strconv.Unquote(imported.Path.Value)
					if err != nil {
						return err
					}
					if forbiddenImport(importPath) && !(dir == "../jobs" &&
						(importPath == modulePath+"/internal/jobs" || importPath == modulePath+"/internal/store")) {
						t.Errorf("%s imports forbidden %q", path, importPath)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", dir, err)
			}
		})
	}
}

func forbiddenImport(path string) bool {
	return path == modulePath+"/internal/runtime/tmux" ||
		path == modulePath+"/internal/runtime/herdr" ||
		path == modulePath+"/internal/tui" ||
		path == modulePath+"/internal/jobs" ||
		path == modulePath+"/internal/store" ||
		path == "os/exec"
}
