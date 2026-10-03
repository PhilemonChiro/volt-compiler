package compiler

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// The lexer reproduces Lark's contextual lexer for grammar.lark. Lark does
// not take the longest match: in each parser state it tries only the
// terminals that state accepts, in priority order, first match wins. The
// visible consequences, all reproduced here:
//
//   - Where the parser accepts NAME, a word is lexed whole and becomes a
//     keyword only if it equals one the state accepts ("if" is a keyword at
//     statement start, a variable name in an expression).
//   - Where NAME is not accepted, keywords match as prefixes: at top level
//     "funcmain()" lexes as `func main()`.
//   - VAR_KEYWORD (priority 4) and TIMEOUT_ELSE (priority 5, also used for
//     if/else) match as prefixes even where NAME is accepted, so at
//     statement start "variance = 1" lexes as `var iance = 1`.
//
// Instead of Lark's parse states the parser passes a lexCtx describing what
// it accepts at that point; tokens are lexed on demand under that context.

type tokKind int

const (
	tBad tokKind = iota // no terminal matches here
	tEOF
	tNL       // one or more newlines
	tName     // NAME, lexed whole (keywords included; the parser decides)
	tKw       // keyword matched as a prefix (lexCtx.kws) or TIMEOUT_ELSE
	tVarKw    // VAR_KEYWORD: "let" or "var"
	tAssignOp // ASSIGN_OP: "+=", "<<=", ...
	tSym      // punctuation and operators
	tDuration
	tFloat
	tHex
	tBin
	tInt
	tString
	tAnnotation // TASK_ANNOTATION: @critical, @priority, @core(0|1)
	tFreq       // 100k, 1M, 8MHz: an int with a k/M multiplier (bus speeds)
)

type token struct {
	kind       tokKind
	text       string
	start, end int // byte offsets
}

// word reports whether t is the keyword or name s.
func (t token) word(s string) bool {
	return (t.kind == tName || t.kind == tKw) && t.text == s
}

func (t token) sym(s string) bool { return t.kind == tSym && t.text == s }

// lexCtx says which context-dependent terminals the parser accepts next.
type lexCtx struct {
	name   bool     // NAME accepted: words are lexed whole
	kws    []string // without name: keywords to match as prefixes
	varKw  bool     // VAR_KEYWORD accepted
	elseKw bool     // TIMEOUT_ELSE accepted
	assign bool     // ASSIGN_OP accepted
	typ    bool     // inside a type: "<" and ">" never join other characters
}

// prefixCtx is a context without NAME, matching the given keywords.
// Lark tries longer strings first.
func prefixCtx(kws ...string) lexCtx {
	sort.SliceStable(kws, func(i, j int) bool { return len(kws[i]) > len(kws[j]) })
	return lexCtx{kws: kws}
}

var (
	ctxName = lexCtx{name: true}
	ctxType = lexCtx{name: true, typ: true}
	ctxStmt = lexCtx{name: true, varKw: true}
	// After an expression. NAME is never accepted there, so keywords that
	// can follow an expression anywhere match as prefixes (as in Lark,
	// whose LALR states after an expression are shared between contexts).
	ctxOp       = prefixCtx("or", "strict", "when", "step", "debounce", "throttle")
	ctxOpAssign = lexCtx{kws: ctxOp.kws, assign: true}
)

var (
	reDuration   = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?(us|ms|seconds|second|sec|minutes|minute|min|hours|hour|hr|s)`)
	reFreq       = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[kM](Hz)?\b`)
	reFloat      = regexp.MustCompile(`^[0-9]+\.[0-9]+`)
	reHex        = regexp.MustCompile(`^0x[0-9a-fA-F]+`)
	reBin        = regexp.MustCompile(`^0b[01_]+`)
	reInt        = regexp.MustCompile(`^[0-9]+`)
	reAnnotation = regexp.MustCompile(`^@(critical|priority|core\([01]\))`)
	reName       = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*`)
)

var assignOps = []string{"<<=", ">>=", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^="}

var twoCharSyms = []string{"->", "..", "||", "&&", "==", "!=", "<=", ">=", "<<", ">>"}

const oneCharSyms = "{}()[]:,.=+-*/%!~<>|^&"

type lexer struct {
	src        string
	lineStarts []int // byte offset of each line's first byte
}

func newLexer(src string) *lexer {
	lx := &lexer{src: src, lineStarts: []int{0}}
	for i := range len(src) {
		if src[i] == '\n' {
			lx.lineStarts = append(lx.lineStarts, i+1)
		}
	}
	return lx
}

// pos converts a byte offset to Lark's 1-based line and column, where
// columns count characters, not bytes.
func (lx *lexer) pos(off int) (line, col int) {
	i := sort.Search(len(lx.lineStarts), func(i int) bool { return lx.lineStarts[i] > off }) - 1
	return i + 1, utf8.RuneCountInString(lx.src[lx.lineStarts[i]:off]) + 1
}

// region is a token's span. Tokens never contain newlines except tNL.
func (lx *lexer) region(t token) Region {
	l, c := lx.pos(t.start)
	return Region{l, c, l, c + utf8.RuneCountInString(t.text)}
}

// skip returns the offset of the next significant character after off,
// passing over spaces, tabs and comments (%ignore in the grammar).
func (lx *lexer) skip(off int) int {
	s := lx.src
	for off < len(s) {
		switch {
		case s[off] == ' ' || s[off] == '\t':
			off++
		case strings.HasPrefix(s[off:], "//"):
			if i := strings.IndexByte(s[off:], '\n'); i >= 0 {
				off += i
			} else {
				off = len(s)
			}
		case strings.HasPrefix(s[off:], "/*"):
			i := strings.Index(s[off+2:], "*/")
			if i < 0 {
				return off // unterminated: lexes as "/" then "*"
			}
			off += 2 + i + 2
		default:
			return off
		}
	}
	return off
}

// lex returns the token at off under ctx, trying terminals in Lark's order.
func (lx *lexer) lex(off int, ctx lexCtx) token {
	off = lx.skip(off)
	s := lx.src[off:]
	tok := func(k tokKind, n int) token { return token{k, s[:n], off, off + n} }
	re := func(k tokKind, r *regexp.Regexp) (token, bool) {
		if m := r.FindString(s); m != "" {
			return tok(k, len(m)), true
		}
		return token{}, false
	}
	if s == "" {
		return token{kind: tEOF, start: off, end: off}
	}
	// Priority > 0 terminals first, highest priority first.
	if ctx.elseKw && strings.HasPrefix(s, "else") {
		return tok(tKw, 4)
	}
	if ctx.varKw && (strings.HasPrefix(s, "let") || strings.HasPrefix(s, "var")) {
		return tok(tVarKw, 3)
	}
	if ctx.assign {
		for _, op := range assignOps {
			if strings.HasPrefix(s, op) {
				return tok(tAssignOp, len(op))
			}
		}
	}
	for _, r := range []struct {
		k  tokKind
		re *regexp.Regexp
	}{{tDuration, reDuration}, {tFreq, reFreq}, {tFloat, reFloat}, {tHex, reHex}, {tBin, reBin}, {tInt, reInt}} {
		if t, ok := re(r.k, r.re); ok {
			return t
		}
	}
	// Priority 0.
	if !ctx.name {
		for _, kw := range ctx.kws {
			if strings.HasPrefix(s, kw) {
				return tok(tKw, len(kw))
			}
		}
	}
	switch c := s[0]; {
	case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		if ctx.name {
			t, _ := re(tName, reName)
			return t
		}
		return tok(tBad, 1)
	case c == '"':
		// ESCAPED_STRING: up to the first quote preceded by an even
		// number of backslashes, on the same line.
		for i := 1; i < len(s) && s[i] != '\n'; i++ {
			if s[i] == '"' {
				n := 0
				for j := i - 1; j > 0 && s[j] == '\\'; j-- {
					n++
				}
				if n%2 == 0 {
					return tok(tString, i+1)
				}
			}
		}
		return tok(tBad, 1)
	case c == '\n' || strings.HasPrefix(s, "\r\n"):
		n := 0
		for n < len(s) {
			if s[n] == '\n' {
				n++
			} else if strings.HasPrefix(s[n:], "\r\n") {
				n += 2
			} else {
				break
			}
		}
		return tok(tNL, n)
	case c == '@':
		if t, ok := re(tAnnotation, reAnnotation); ok {
			return t
		}
		return tok(tBad, 1)
	}
	for _, sym := range twoCharSyms {
		if ctx.typ && (sym[0] == '<' || sym[0] == '>') {
			continue
		}
		if strings.HasPrefix(s, sym) {
			return tok(tSym, 2)
		}
	}
	if strings.IndexByte(oneCharSyms, s[0]) >= 0 {
		return tok(tSym, 1)
	}
	return tok(tBad, 1)
}
