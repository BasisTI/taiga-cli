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
	"emit*/renderPresented":               {"present.go"},
	"rawOut":                              {"present.go", "api.go", "attachment.go"},
	"cobra writer (SetOut, OutOrStdout…)": {"present.go"},
}

var cobraWriters = map[string]bool{"OutOrStdout": true, "OutOrStderr": true, "ErrOrStderr": true, "SetOut": true, "SetErr": true,
	"SetOutput": true, "Print": true, "Println": true, "Printf": true, "PrintErr": true, "PrintErrln": true, "PrintErrf": true}

// completionHooks would let completion print text of its own (server names, errors) through
// CompErrorln, which completeRedacted cannot redact.
var completionHooks = map[string]bool{"ValidArgsFunction": true, "RegisterFlagCompletionFunc": true}

// compWriters are Cobra's completion printers: os.Stderr and $BASH_COMP_DEBUG_FILE, unredacted.
var compWriters = map[string]bool{"CompDebug": true, "CompDebugln": true, "CompError": true, "CompErrorln": true}

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
		if name == "." && (path == "os" || path == "fmt" || path == "github.com/spf13/cobra") {
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
		case *ast.Ident:
			// A field (ValidArgsFunction: f), a method or a selector: every use of the name.
			if completionHooks[x.Name] {
				add(x, x.Name+": completion errors go to os.Stderr unredacted (see completion.go)")
			}
		case *ast.SelectorExpr:
			name := x.Sel.Name
			if pkg, ok := x.X.(*ast.Ident); ok && imports[pkg.Name] != "" {
				switch {
				case imports[pkg.Name] == "os" && (name == "Stdout" || name == "Stderr") && !allowed("os.Stdout/os.Stderr", file):
					add(x, "os."+name+" outside the constructor")
				case imports[pkg.Name] == "fmt" && strings.HasPrefix(name, "Print"):
					add(x, "fmt."+name+" writes to stdout")
				case imports[pkg.Name] == "github.com/spf13/cobra" && compWriters[name]:
					add(x, "cobra."+name+" writes to os.Stderr and $BASH_COMP_DEBUG_FILE")
				}
				return true
			}
			switch {
			case (name == "Out" || name == "Err") && !allowed("App.Out/App.Err", file):
				add(x, "."+name+" outside present.go")
			case (strings.HasPrefix(name, "emit") || name == "renderPresented") && !allowed("emit*/renderPresented", file):
				add(x, name+" (writes without redacting) outside present.go")
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
		`package cli; import "github.com/spf13/cobra"; func f() { cobra.CompErrorln("x") }`,
		`package cli; import c "github.com/spf13/cobra"; func f() { c.CompDebug("x", true) }`,
		`package cli; import . "github.com/spf13/cobra"; func f() { CompErrorln("x") }`,
		`package cli; import "github.com/spf13/cobra"; func f() { _ = &cobra.Command{ValidArgsFunction: nil} }`,
		`package cli; import "github.com/spf13/cobra"; func f(cmd *cobra.Command) { _ = cmd.RegisterFlagCompletionFunc("x", nil) }`,
	} {
		v, err := outputViolations("review_bypass.go", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if len(v) == 0 {
			t.Errorf("not caught: %s", src)
		}
	}
	// Completion options and types are no printers.
	if v, _ := outputViolations("root.go", []byte(`package cli; import "github.com/spf13/cobra"; var _ = cobra.CompletionOptions{}; var _ cobra.Completion`)); len(v) != 0 {
		t.Errorf("root.go: %v", v)
	}
	// What present.go and the constructor may do stays allowed.
	if v, _ := outputViolations("present.go", []byte(`package cli; func f(a *App) { _ = a.Out; _ = a.Err; a.emitFields(nil); a.root().SetOut(a.Out) }`)); len(v) != 0 {
		t.Errorf("present.go: %v", v)
	}
	if v, _ := outputViolations("cmd/taiga/main.go", []byte(`package main; import "os"; func main() { _ = os.Stdout; _ = os.Stderr }`)); len(v) != 0 {
		t.Errorf("main.go: %v", v)
	}
}

// The password prompt keeps its line break raw: only text is redacted and quoted.
func TestPromptKeepsTheLineBreak(t *testing.T) {
	var out strings.Builder
	a := &App{Err: &out}
	a.prompt("Password: ")
	a.prompt("\n")
	a.prompt("see http://h/p?token=PROMPTSECRET\n")
	if got := out.String(); got != "Password: \nsee http://h/p?token=…\n" {
		t.Fatalf("%q", got)
	}
}

// Every raw emitter is blocked outside present.go: emitOut too, and any new emit* by name.
func TestOutputGuardBlocksEveryEmitter(t *testing.T) {
	for _, src := range []string{
		`package cli; func f(a *App) error { return a.emitOut("token=NEW_GUARD_SECRET\n") }`,
		`package cli; func f(a *App) error { return a.emitSomethingNew("x") }`,
		`package cli; func f(a *App) error { return a.renderPresented(nil, nil) }`,
	} {
		if v, _ := outputViolations("review_emit_bypass.go", []byte(src)); len(v) == 0 {
			t.Errorf("not caught: %s", src)
		}
	}
}

// In present.go itself, a function that writes to App.Out or App.Err without redacting must be
// an emitter (emit*, blocked elsewhere by name), rawOut or wireOutput; any other writer there
// must redact, which this test cannot see, so new ones have to be added here on purpose.
func TestPresentWritersAreKnown(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "present.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"rawOut": true, "wireOutput": true, "writeError": true}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		touches := false
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok && (s.Sel.Name == "Out" || s.Sel.Name == "Err") {
				touches = true
			}
			return true
		})
		if touches && !strings.HasPrefix(fn.Name.Name, "emit") && !known[fn.Name.Name] {
			t.Errorf("%s writes to App.Out/App.Err: name it emit* (raw) or add it to known after checking it redacts", fn.Name.Name)
		}
	}
}
