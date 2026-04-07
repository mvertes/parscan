package goparser

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/mvertes/parscan/lang"
	"github.com/mvertes/parscan/symbol"
	"github.com/mvertes/parscan/vm"
)

func (p *Parser) importSrc(pkgPath string) (out Tokens, err error) {
	r, err := p.ParseAll(pkgPath, "")
	if err != nil {
		return out, err
	}
	for _, s := range r {
		out = append(out, s...)
	}
	return out, err
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
				buf, err := fs.ReadFile(p.pkgfs, name+"/"+f.Name())
				if err != nil {
					return out, err
				}
				src := string(buf)
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

	// Pre-register struct type placeholders so that forward and mutual
	// references (e.g., type F func(*A); type A struct{F}) can resolve.
	// Placeholders are untracked: they survive the retry loop cleanup.
	p.preRegisterStructTypes(decls)

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
					for _, k := range p.SymTracker {
						delete(p.Symbols, k)
					}
					p.SymTracker = nil
					retry = append(retry, decl)
					if firstErr == nil {
						firstErr = parseErr
					}
					continue
				}
				return out, parseErr
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

func (p *Parser) preRegisterStructTypes(decls []Tokens) {
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
				if len(lt) >= 2 && lt[0].Tok == lang.Ident && lt[1].Tok == lang.Struct {
					p.registerStructPlaceholder(lt[0].Str)
				}
			}
			continue
		}
		// Single: type A struct{...}
		if len(decl) >= 3 && decl[1].Tok == lang.Ident && decl[2].Tok == lang.Struct {
			p.registerStructPlaceholder(decl[1].Str)
		}
	}
}

func (p *Parser) registerStructPlaceholder(name string) {
	if _, ok := p.Symbols[name]; ok {
		return // Already registered (e.g., from a previous Compile call).
	}
	ph := vm.NewStructType()
	ph.Name = name
	p.SymAdd(symbol.UnsetAddr, name, vm.NewValue(ph.Rtype), symbol.Type, ph)
}
