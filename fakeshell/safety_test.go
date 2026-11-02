package fakeshell

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports are packages through which a fake session could reach the
// real machine: processes, files, sockets, raw syscalls, or memory tricks.
var forbiddenImports = []string{
	"os", "os/exec", "os/signal", "os/user",
	"io/ioutil", "path/filepath",
	"net", "net/http", "net/rpc",
	"syscall", "golang.org/x/sys",
	"unsafe", "plugin", "runtime/cgo", "C",
	"github.com/creack/pty",
	"github.com/smart-bit-me/scomp-agent/internal/pty",
	"github.com/smart-bit-me/scomp-agent/internal/tmuxsess",
	"github.com/smart-bit-me/scomp-agent/internal/screensess",
}

func forbidden(imp string) bool {
	for _, f := range forbiddenImports {
		if imp == f || strings.HasPrefix(imp, f+"/") {
			return true
		}
	}
	return false
}

// TestNoRealSystemAccess fails if any non-test file of fakeshell or its
// subpackages imports a package that could execute, open or connect to
// anything real. This is the guarantee docs/FAKESHELL.md relies on.
func TestNoRealSystemAccess(t *testing.T) {
	files := 0
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "honeyfs" {
			return filepath.SkipDir // embedded data, not code
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		files++
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if forbidden(p) {
				t.Errorf("%s imports %q: fakeshell must not reach the real system", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 5 {
		t.Fatalf("scanned only %d files; is the walk rooted correctly?", files)
	}
}

func TestHoneyfsHasNoGoSource(t *testing.T) {
	err := fs.WalkDir(honeyfs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (strings.HasSuffix(path, ".go") || d.Name() == "go.mod") {
			t.Errorf("honeyfs/%s would be treated as Go source; put it in staticFiles", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
