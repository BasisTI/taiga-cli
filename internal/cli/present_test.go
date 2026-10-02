package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// outputRule is where each output-related name may appear. present.go is the presentation
// layer: the only code that touches the process writers, App.Out and App.Err.
var outputRule = map[string][]string{
	"App.Out/App.Err":                     {"present.go"},
	"os.Stdout/os.Stderr":                 {"cmd/taiga/main.go"},
	"emitFields/emitJSON/emitErr":         {"present.go"},
	"rawOut":                              {"present.go", "api.go", "attachment.go"},
	"cobra writer (SetOut, OutOrStdout…)": {"present.go"},
}

var cobraWriters = map[string]bool{"OutOrStdout": true, "OutOrStderr": true, "ErrOrStderr": true, "SetOut": true, "SetErr": true,
	"SetOutput": true, "Print": true, "Println": true, "Printf": true, "PrintErr": true, "PrintErrln": true, "PrintErrf": true}

func allowed(rule, file string) bool {
	for _, f := range outputRule[rule] {
		if file == f {
			return true
		}
	}
	return false
}

// outputViolations reads the syntax tree of a file, so spacing, aliases (dst := a.Out) and
// import names (o "os") do not hide a reference: every use is found where it is taken.
func outputViolations(file string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return nil, err
	}
	imports := map[string]string{} // local name → path
	out := []string{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := filepath.Base(path)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "." && (path == "os" || path == "fmt") {
			out = append(out, "dot import of "+path)
		}
		if path == "log" {
			out = append(out, "imports log (writes to stderr)")
		}
		imports[name] = path
	}
	add := func(n ast.Node, msg string) {
		out = append(out, fset.Position(n.Pos()).String()+": "+msg)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
				add(x, "builtin "+id.Name+" writes to stderr")
			}
		case *ast.SelectorExpr:
			name := x.Sel.Name
			if pkg, ok := x.X.(*ast.Ident); ok && imports[pkg.Name] != "" {
				switch {
				case imports[pkg.Name] == "os" && (name == "Stdout" || name == "Stderr") && !allowed("os.Stdout/os.Stderr", file):
					add(x, "os."+name+" outside the constructor")
				case imports[pkg.Name] == "fmt" && strings.HasPrefix(name, "Print"):
					add(x, "fmt."+name+" writes to stdout")
				}
				return true
			}
			switch {
			case (name == "Out" || name == "Err") && !allowed("App.Out/App.Err", file):
				add(x, "."+name+" outside present.go")
			case (name == "emitFields" || name == "emitJSON" || name == "emitErr") && !allowed("emitFields/emitJSON/emitErr", file):
				add(x, name+" outside present.go")
			case name == "rawOut" && !allowed("rawOut", file):
				add(x, "rawOut outside api.go and attachment.go")
			case cobraWriters[name] && !allowed("cobra writer (SetOut, OutOrStdout…)", file):
				add(x, "cobra writer "+name+" outside present.go")
			}
		}
		return true
	})
	return out, nil
}

// Every write to stdout and stderr goes through present.go, which redacts once; only the
// constructor names the process streams. The check runs on the syntax tree of every
// production file of the CLI and of cmd/taiga.
func TestOutputGoesThroughPresent(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	files = append(files, filepath.Join("..", "..", "cmd", "taiga", "main.go"))
	sort.Strings(files)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := path
		if strings.HasSuffix(path, filepath.Join("cmd", "taiga", "main.go")) {
			name = "cmd/taiga/main.go"
		}
		v, err := outputViolations(name, src)
		if err != nil {
			t.Fatal(err)
		}
		for _, msg := range v {
			t.Errorf("%s", msg)
		}
	}
}

// The checker itself: each way around the old substring guard is caught.
func TestOutputGuardCatchesBypasses(t *testing.T) {
	for _, src := range []string{
		`package cli; import "fmt"; func f(a *App) { dst := a.Out; fmt.Fprintln(dst, "x") }`,
		`package cli; import "fmt"; func f(a *App) { fmt.Fprintln( a.Out, "x") }`,
		`package cli; import ("fmt"; "os"); func f() { dst := os.Stdout; fmt.Fprintln(dst, "x") }`,
		`package cli; import ("fmt"; o "os"); func f() { fmt.Fprintln(o.Stderr, "x") }`,
		`package cli; import "fmt"; func f(a *App) { cmd := a.root(); fmt.Fprintln(cmd.OutOrStdout(), "x") }`,
		`package cli; func f(a *App) { a.root().Println("x") }`,
		`package cli; import "fmt"; func f() { fmt.Println("x") }`,
		`package cli; import "log"; func f() { log.Print("x") }`,
		`package cli; func f() { println("x") }`,
		`package cli; func f(a *App) { a.emitFields(nil) }`,
		`package cli; func f(a *App) { _ = a.rawOut() }`,
		`package cli; func f(a *App) error { return a.writeError(nil) }; var _ = (*App).emitJSON`,
	} {
		v, err := outputViolations("review_bypass.go", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if len(v) == 0 {
			t.Errorf("not caught: %s", src)
		}
	}
	// What present.go and the constructor may do stays allowed.
	if v, _ := outputViolations("present.go", []byte(`package cli; func f(a *App) { _ = a.Out; _ = a.Err; a.emitFields(nil); a.root().SetOut(a.Out) }`)); len(v) != 0 {
		t.Errorf("present.go: %v", v)
	}
	if v, _ := outputViolations("cmd/taiga/main.go", []byte(`package main; import "os"; func main() { _ = os.Stdout; _ = os.Stderr }`)); len(v) != 0 {
		t.Errorf("main.go: %v", v)
	}
}
