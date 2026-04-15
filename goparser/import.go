package goparser

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"unicode"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

func (p *Parser) importSrc(pkgPath string) (err error) {
	// Save and restore parser state so the imported package's
	// "package" declaration does not conflict with the current one.
	savedPkgName := p.pkgName
	p.pkgName = ""
	defer func() { p.pkgName = savedPkgName }()

	// Snapshot existing symbol keys so we can identify new ones.
	existing := make(map[string]bool, len(p.Symbols))
	for k := range p.Symbols {
		existing[k] = true
	}

	remaining, err := p.ParseAll(pkgPath, "")
	if err != nil {
		return err
	}

	// Store remaining declarations (func bodies, var initializers)
	// for code generation by the outer ParseAll / Compile.
	p.importRemaining = append(p.importRemaining, remaining...)

	// Collect exported symbols into a Package entry and create
	// qualified aliases (e.g. "example.com/pkg1.V") so the compiler
	// can resolve pkg.Member accesses.
	pkg := &symbol.Package{
		Path:   pkgPath,
		Values: map[string]vm.Value{},
	}
	for k, s := range p.Symbols {
		if existing[k] || !IsExported(k) {
			continue
		}
		pkg.Values[k] = s.Value
	}
	// Create qualified aliases after the loop to avoid mutating p.Symbols during iteration.
	for k := range pkg.Values {
		p.Symbols[pkgPath+"."+k] = p.Symbols[k]
	}
	p.Packages[pkgPath] = pkg

	return nil
}

// ParseAll parses code and its dependencies, and returns slices of Tokens or an error.
func (p *Parser) ParseAll(name, src string) (out []Tokens, err error) {
	var decls []Tokens

	if src == "" {
		// Get content from file(s).
		if p.pkgfs == nil {
			p.pkgfs = os.DirFS(".")
		}
		fi, err := fs.Stat(p.pkgfs, name)
		if err != nil {
			return out, err
		}
		if fi.IsDir() {
			files, err := fs.ReadDir(p.pkgfs, name)
			if err != nil {
				return out, err
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
					continue
				}
				if !MatchFileName(f.Name(), p.buildCtx) {
					continue
				}
				buf, err := fs.ReadFile(p.pkgfs, name+"/"+f.Name())
				if err != nil {
					return out, err
				}
				src := string(buf)
				if !matchBuildDirective(src, p.buildCtx) {
					continue
				}
				d, err := p.scanDecls(src)
				p.PosBase = p.Sources.Add(name, src)
				if err != nil {
					return out, err
				}
				decls = append(decls, d...)
			}
		}
	} else {
		decls, err = p.scanDecls(src)
		p.PosBase = p.Sources.Add(name, src)
		if err != nil {
			return out, err
		}
	}

	// Pre-register struct and interface type placeholders so that forward,
	// mutual, and self-references can resolve during parsing.
	// Placeholders are untracked: they survive the retry loop cleanup.
	p.preRegisterTypes(decls)

	// Phase 1: resolve all declarations (no code generation).
	// Retry until no undefined declaration remains, or no progress is made.
	var remaining []Tokens // decls needing full parse + generate
	pending := decls
	for len(pending) > 0 {
		var retry []Tokens
		var firstErr error
		for _, decl := range pending {
			p.SymTracker = nil
			handled, parseErr := p.ParseDecl(decl)
			if parseErr != nil {
				var eu ErrUndefined
				if errors.As(parseErr, &eu) {
					p.rollbackSymTracker()
					retry = append(retry, decl)
					if firstErr == nil {
						firstErr = parseErr
					}
					continue
				}
				// Propagate I/O and filesystem errors (e.g. missing packages).
				// Skip everything else (parser limitations, unimplemented syntax).
				var pathErr *fs.PathError
				if errors.As(parseErr, &pathErr) {
					return out, parseErr
				}
				p.rollbackSymTracker()
				if firstErr == nil {
					firstErr = parseErr
				}
				continue
			}
			if !handled {
				remaining = append(remaining, decl)
			}
		}
		if len(retry) == len(pending) {
			return out, firstErr
		}
		pending = retry
	}

	// Include code-gen declarations from imported source packages.
	if len(p.importRemaining) > 0 {
		remaining = append(p.importRemaining, remaining...)
		p.importRemaining = nil
	}

	// Phase 2: split var blocks, sort var declarations by dependency,
	// then generate code in two passes. All symbols (including methods)
	// are registered in Phase 1 with their signatures.
	//
	// Pass 1 compiles var initializers so that all var types are resolved.
	// Pass 2 compiles func bodies and expression statements; by then every
	// global var has a concrete type, eliminating forward-reference retries.
	remaining = p.SplitAndSortVarDecls(remaining)
	return remaining, err
}

func (p *Parser) preRegisterTypes(decls []Tokens) {
	for _, decl := range decls {
		if len(decl) < 2 || decl[0].Tok != lang.Type {
			continue
		}
		if decl[1].Tok == lang.ParenBlock {
			// Grouped: type ( A struct{...}; B struct{...} )
			inner, err := p.Scan(decl[1].Block(), false)
			if err != nil {
				continue
			}
			for _, lt := range inner.Split(lang.Semicolon) {
				if len(lt) >= 2 && lt[0].Tok == lang.Ident {
					n := lt[0].Str
					switch lt[1].Tok {
					case lang.Struct:
						p.registerStructPlaceholder(n, n)
					case lang.Interface:
						p.registerInterfacePlaceholder(n, n)
					}
				}
			}
			continue
		}
		// Single: type A struct{...} or type A interface{...}
		if len(decl) >= 3 && decl[1].Tok == lang.Ident {
			n := decl[1].Str
			switch decl[2].Tok {
			case lang.Struct:
				p.registerStructPlaceholder(n, n)
			case lang.Interface:
				p.registerInterfacePlaceholder(n, n)
			}
		}
	}
}

// registerStructPlaceholder returns an existing or new struct placeholder.
// key is the symbol table key (possibly scoped); short is the unqualified type name.
func (p *Parser) registerStructPlaceholder(key, short string) *vm.Type {
	if s, ok := p.Symbols[key]; ok && s.Kind == symbol.Type {
		return s.Type
	}
	ph := vm.NewStructType()
	ph.Name = short
	p.SymAdd(symbol.UnsetAddr, key, vm.NewValue(ph.Rtype), symbol.Type, ph)
	return ph
}

// registerInterfacePlaceholder returns an existing or new interface placeholder.
// key is the symbol table key (possibly scoped); short is the unqualified type name.
func (p *Parser) registerInterfacePlaceholder(key, short string) *vm.Type {
	if s, ok := p.Symbols[key]; ok && s.Kind == symbol.Type {
		return s.Type
	}
	ph := &vm.Type{Rtype: vm.AnyRtype, Name: short}
	p.SymAdd(symbol.UnsetAddr, key, vm.NewValue(ph.Rtype), symbol.Type, ph)
	return ph
}

func (p *Parser) rollbackSymTracker() {
	for _, k := range p.SymTracker {
		delete(p.Symbols, k)
	}
	p.SymTracker = nil
}

// IsExported reports whether the given name starts with an upper-case letter.
func IsExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}
