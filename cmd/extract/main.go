// Command extract parses Go package source using parscan's goparser and prints
// exported const, var, type, and func declarations to stdout.
//
// Usage:
//
//	go run ./cmd/extract <directory>
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvertes/parscan/goparser"
	"github.com/mvertes/parscan/lang/golang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: extract <directory>")
		os.Exit(1)
	}

	if err := run(flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	// Pre-scan source files to discover import paths.
	imports, err := extractImports(dir)
	if err != nil {
		return fmt.Errorf("scanning imports: %w", err)
	}

	p := goparser.NewParser(golang.GoSpec, false)

	// Register stub packages for all imports so the parser skips source resolution.
	for _, imp := range imports {
		p.Packages[imp] = &symbol.Package{
			Path:   imp,
			Bin:    true,
			Values: map[string]vm.Value{},
		}
	}

	p.SetPkgfs(filepath.Dir(dir))
	if _, err := p.ParseAll(filepath.Base(dir), ""); err != nil {
		fmt.Fprintln(os.Stderr, "warning:", err)
	}

	// Collect exported top-level symbols grouped by kind.
	groups := map[symbol.Kind][]string{
		symbol.Const: {},
		symbol.Var:   {},
		symbol.Type:  {},
		symbol.Func:  {},
	}

	for name, sym := range p.Symbols {
		if strings.ContainsAny(name, "/.") {
			continue
		}
		if !goparser.IsExported(name) {
			continue
		}
		if _, ok := groups[sym.Kind]; !ok {
			continue
		}
		groups[sym.Kind] = append(groups[sym.Kind], name)
	}

	for _, kind := range []symbol.Kind{symbol.Const, symbol.Var, symbol.Type, symbol.Func} {
		names := groups[kind]
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("%s %s\n", strings.ToLower(kind.String()), name)
		}
	}

	return nil
}

// extractImports reads all .go files in dir (excluding _test.go) and returns
// the deduplicated list of import paths found.
func extractImports(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if !goparser.MatchFileName(e.Name(), nil) {
			continue
		}
		buf, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, imp := range scanImports(string(buf)) {
			seen[imp] = true
		}
	}

	imports := make([]string, 0, len(seen))
	for imp := range seen {
		imports = append(imports, imp)
	}
	sort.Strings(imports)
	return imports, nil
}

// scanImports extracts import paths from Go source text using simple line scanning.
func scanImports(src string) []string {
	var imports []string
	inBlock := false

	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)

		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			if p := extractQuoted(line); p != "" {
				imports = append(imports, p)
			}
			continue
		}

		if strings.HasPrefix(line, "import (") {
			inBlock = true
			continue
		}
		if strings.HasPrefix(line, "import ") {
			if p := extractQuoted(line); p != "" {
				imports = append(imports, p)
			}
		}
	}
	return imports
}

// extractQuoted returns the first double-quoted string found in line.
func extractQuoted(line string) string {
	i := strings.IndexByte(line, '"')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(line[i+1:], '"')
	if j < 0 {
		return ""
	}
	return line[i+1 : i+1+j]
}
