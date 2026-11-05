// Command testdoc keeps docs/TESTING.md and the test code in one-to-one
// correspondence.
//
// The test code is the source of truth. Every Test, Benchmark and Fuzz
// function declares the checkpoints it covers with a directive in its doc
// comment:
//
//	// Checks: UDP-21, UDP-22
//	func TestReplyValidatesPacket(t *testing.T) {
//
// testdoc regenerates the "测试" column of every checkpoint table from those
// directives and enforces:
//
//   - every test function has a Checks directive;
//   - every ID in a directive is a checkpoint in docs/TESTING.md;
//   - a checkpoint marked ✅ or ⚠️ has at least one test;
//   - a checkpoint marked ❌, 🔧, ➖ or ⚙️ (enforced by a command, not a
//     test function) has none;
//   - every checkpoint a REG row points to exists;
//   - docs/TESTING.md is up to date with the code;
//   - every package has a design document in docs/design (DOC-02): the
//     root package in runtime.md, internal packages as "## internal/<pkg>"
//     sections of internal.md, every other package in <name>.md.
//
// Usage, from the repository root:
//
//	go run ./internal/tools/testdoc        # check only
//	go run ./internal/tools/testdoc -w     # rewrite docs/TESTING.md
//
// The same check runs as a unit test, so `go test ./...` fails whenever
// the document and the tests drift apart.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const docPath = "docs/TESTING.md"

var (
	idRe        = regexp.MustCompile(`^[A-Z]+-\d+$`)
	directiveRe = regexp.MustCompile(`^Checks:\s*(.+)$`)
)

// Test is one test function and the checkpoints it declares.
type Test struct {
	Pkg, Name, Pos string
	IDs            []string
}

// Label is how the test appears in the document, e.g. udp.ReplyValidatesPacket.
func (t Test) Label() string {
	return t.Pkg + "." + strings.TrimPrefix(t.Name, "Test")
}

// Row is one checkpoint row: | ID | … | 状态 | 测试 | 说明 |.
type Row struct {
	Line   int // index into the document's lines
	Cells  []string
	ID     string
	Status string
}

func main() {
	write := flag.Bool("w", false, "rewrite "+docPath)
	flag.Parse()
	problems, err := run(".", *write)
	if err != nil {
		fmt.Fprintln(os.Stderr, "testdoc:", err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}

// run checks root and, with write, regenerates the document first.
func run(root string, write bool) ([]string, error) {
	tests, problems, err := collectTests(root)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, docPath))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(raw), "\n")
	rows, regRefs := parseDoc(lines)

	byID := map[string][]Test{}
	for _, t := range tests {
		for _, id := range t.IDs {
			if _, ok := rows[id]; !ok {
				problems = append(problems, fmt.Sprintf("%s: %s declares unknown checkpoint %s", t.Pos, t.Name, id))
				continue
			}
			byID[id] = append(byID[id], t)
		}
	}

	ids := make([]string, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := rows[id]
		n := len(byID[id])
		switch {
		case (r.Status == "✅" || r.Status == "⚠️") && n == 0:
			problems = append(problems, fmt.Sprintf("%s:%d: %s is %s but no test declares it", docPath, r.Line+1, id, r.Status))
		case (r.Status == "❌" || r.Status == "🔧" || r.Status == "➖" || r.Status == "⚙️") && n > 0:
			problems = append(problems, fmt.Sprintf("%s:%d: %s is %s but %d test(s) declare it; update the status", docPath, r.Line+1, id, r.Status, n))
		}
		want := render(byID[id])
		if r.Cells[len(r.Cells)-2] != want {
			if write {
				r.Cells[len(r.Cells)-2] = want
				lines[r.Line] = "| " + strings.Join(r.Cells, " | ") + " |"
			} else {
				problems = append(problems, fmt.Sprintf("%s:%d: %s test column is stale; run go run ./internal/tools/testdoc -w", docPath, r.Line+1, id))
			}
		}
	}
	for _, ref := range regRefs {
		if _, ok := rows[ref.id]; !ok {
			problems = append(problems, fmt.Sprintf("%s:%d: regression row points to unknown checkpoint %s", docPath, ref.line+1, ref.id))
		}
	}

	design, err := checkDesignDocs(root)
	if err != nil {
		return nil, err
	}
	problems = append(problems, design...)

	if write {
		if err := os.WriteFile(filepath.Join(root, docPath), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			return nil, err
		}
	}
	return problems, nil
}

func render(tests []Test) string {
	labels := make([]string, len(tests))
	for i, t := range tests {
		labels[i] = "`" + t.Label() + "`"
	}
	sort.Strings(labels)
	return strings.Join(labels, "<br>")
}

type regRef struct {
	id   string
	line int
}

// parseDoc finds checkpoint rows (tables whose header ends in "测试 | 说明")
// and the checkpoint IDs referenced from the regression table.
func parseDoc(lines []string) (map[string]*Row, []regRef) {
	rows := map[string]*Row{}
	var refs []regRef
	inCheckpoints, inRegression := false, false
	for i, l := range lines {
		if !strings.HasPrefix(l, "|") {
			inCheckpoints, inRegression = false, false
			continue
		}
		cells := split(l)
		if len(cells) == 0 {
			continue
		}
		switch {
		case len(cells) >= 2 && cells[len(cells)-2] == "测试" && cells[len(cells)-1] == "说明":
			inCheckpoints = true
			continue
		case cells[0] == "ID" && contains(cells, "问题"):
			inRegression = true
			continue
		}
		if !idRe.MatchString(cells[0]) {
			continue
		}
		if inCheckpoints {
			rows[cells[0]] = &Row{Line: i, Cells: cells, ID: cells[0], Status: statusOf(cells)}
		}
		if inRegression && len(cells) >= 4 {
			for _, part := range regexp.MustCompile(`[A-Z]+-\d+`).FindAllString(cells[3], -1) {
				refs = append(refs, regRef{part, i})
			}
		}
	}
	return rows, refs
}

func split(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func contains(cells []string, s string) bool {
	for _, c := range cells {
		if c == s {
			return true
		}
	}
	return false
}

// statusOf returns the status cell, the one before 测试 and 说明.
func statusOf(cells []string) string {
	if len(cells) < 3 {
		return ""
	}
	return cells[len(cells)-3]
}

// collectTests parses every _test.go file, ignoring build tags, so the
// result is the same on every platform.
func collectTests(root string) ([]Test, []string, error) {
	var tests []Test
	var problems []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		pkg := strings.TrimSuffix(f.Name.Name, "_test")
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !isTestFunc(fn.Name.Name) {
				continue
			}
			t := Test{Pkg: pkg, Name: fn.Name.Name, Pos: fset.Position(fn.Pos()).String()}
			t.IDs = directive(fn.Doc)
			if len(t.IDs) == 0 {
				problems = append(problems, fmt.Sprintf("%s: %s has no // Checks: directive", t.Pos, t.Name))
			}
			tests = append(tests, t)
		}
		return nil
	})
	return tests, problems, err
}

func isTestFunc(name string) bool {
	for _, p := range []string{"Test", "Benchmark", "Fuzz"} {
		if strings.HasPrefix(name, p) && (len(name) == len(p) || name[len(p)] >= 'A' && name[len(p)] <= 'Z') {
			return true
		}
	}
	return false
}

func directive(doc *ast.CommentGroup) []string {
	if doc == nil {
		return nil
	}
	var ids []string
	for _, c := range doc.List {
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		m := directiveRe.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		for _, id := range strings.Split(m[1], ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// checkDesignDocs reports packages without a design document.
func checkDesignDocs(root string) ([]string, error) {
	var problems []string
	internal, _ := os.ReadFile(filepath.Join(root, "docs/design/internal.md"))
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(d.Name(), ".") && rel != "." || d.Name() == "testdata" || rel == "examples" || strings.HasPrefix(rel, "examples/") || rel == "docs" {
			return filepath.SkipDir
		}
		if !hasPackage(path) {
			return nil
		}
		switch {
		case rel == ".":
			if !exists(filepath.Join(root, "docs/design/runtime.md")) {
				problems = append(problems, "docs/design/runtime.md: missing design document for the root package")
			}
		case strings.HasPrefix(rel, "internal/"):
			if !strings.Contains(string(internal), "## "+rel+" ") && !strings.Contains(string(internal), "## "+rel+"\n") {
				problems = append(problems, fmt.Sprintf("docs/design/internal.md: missing section \"## %s\"", rel))
			}
		default:
			if !exists(filepath.Join(root, "docs/design", rel+".md")) {
				problems = append(problems, fmt.Sprintf("docs/design/%s.md: missing design document for package %s", rel, rel))
			}
		}
		return nil
	})
	return problems, err
}

// hasPackage reports whether dir holds non-test Go files.
func hasPackage(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			return true
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
