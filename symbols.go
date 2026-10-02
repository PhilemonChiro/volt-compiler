package compiler

// Port of symbols.py: the symbol table.

// Symbol is a named thing in the program.
type Symbol struct {
	Name       string
	Kind       string // "pin", "sensor", "bus", "func", "task", "param", "var", "struct", "enum", "machine", "storage", "channel", "builtin"
	TypeName   string // "int", "float", "Output", "BME280", "func", etc.
	Region     Region // where declared (for error messages)
	Mutable    bool
	Params     []SymParam // for functions
	ReturnType *string    // for functions; nil = void
}

type SymParam struct{ Name, TypeName string }

// Scope is a lexical scope. Names keep declaration order, which
// AllNames (and so "did you mean?" suggestions) depends on.
type Scope struct {
	Parent  *Scope
	Name    string
	names   []string
	symbols map[string]*Symbol
}

func NewScope(parent *Scope, name string) *Scope {
	return &Scope{Parent: parent, Name: name, symbols: map[string]*Symbol{}}
}

// Declare adds sym to this scope. On a duplicate it returns the existing
// symbol and leaves the scope unchanged; the caller reports the error.
func (s *Scope) Declare(sym *Symbol) *Symbol {
	if existing, ok := s.symbols[sym.Name]; ok {
		return existing
	}
	s.symbols[sym.Name] = sym
	s.names = append(s.names, sym.Name)
	return nil
}

// Lookup finds name here or in an enclosing scope.
func (s *Scope) Lookup(name string) *Symbol {
	for sc := s; sc != nil; sc = sc.Parent {
		if sym, ok := sc.symbols[name]; ok {
			return sym
		}
	}
	return nil
}

// LookupLocal finds name in this scope only.
func (s *Scope) LookupLocal(name string) *Symbol {
	return s.symbols[name]
}

// AllNames lists every visible name, innermost scope first.
func (s *Scope) AllNames() []string {
	var names []string
	for sc := s; sc != nil; sc = sc.Parent {
		names = append(names, sc.names...)
	}
	return names
}

func (s *Scope) Child(name string) *Scope {
	return NewScope(s, name)
}
