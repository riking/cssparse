// Copyright 2018 Kane York.
// Copyright 2012 The Gorilla Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tokenizer

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// TokenType identifies the type of lexical tokens.
type TokenType int

// String returns a string representation of the token type.
func (t TokenType) String() string {
	return tokenNames[t]
}

// Stop tokens are TokenError, TokenEOF, TokenBadEscape,
// TokenBadString, TokenBadURI.  A consumer that does not want to tolerate
// parsing errors should stop parsing when this returns true.
func (t TokenType) StopToken() bool {
	return t == TokenError || t == TokenEOF || t == TokenBadEscape || t ==
		TokenBadString || t == TokenBadURI
}

// ParseError represents a CSS syntax error.
type ParseError struct {
	Type    TokenType
	Message string
	Loc     int
}

// implements error
func (e *ParseError) Error() string {
	return e.Message
}

// Token represents a token in the CSS syntax.
type Token struct {
	Type TokenType
	// A string representation of the token value that depends on the type.
	// For example, for a TokenURI, the Value is the URI itself.  For a
	// TokenPercentage, the Value is the number without the percent sign.
	Value string
	// Extra data for the token beyond a simple string.  Will always be a
	// pointer to a "TokenExtra*" type in this package.
	Extra TokenExtra
}

// The complete list of tokens in CSS Syntax Level 3.
const (
	// Scanner flags.
	TokenError TokenType = iota
	TokenEOF

	// Tokens
	TokenIdent
	TokenFunction
	TokenURI
	TokenDelim // Single character
	TokenAtKeyword
	TokenString
	TokenS // Whitespace
	// CSS Syntax Level 3 removes comments from the token stream, but they are
	// preserved here.
	TokenComment

	// Extra data: TokenExtraHash
	TokenHash
	// Extra data: TokenExtraNumeric
	TokenNumber
	TokenPercentage
	TokenDimension
	// Extra data: TokenExtraUnicodeRange
	TokenUnicodeRange

	// Error tokens
	TokenBadString
	TokenBadURI
	TokenBadEscape // a '\' right before a newline

	// Fixed-string tokens
	TokenIncludes
	TokenDashMatch
	TokenPrefixMatch
	TokenSuffixMatch
	TokenSubstringMatch
	TokenColumn
	TokenColon
	TokenSemicolon
	TokenComma
	TokenOpenBracket
	TokenCloseBracket
	TokenOpenParen
	TokenCloseParen
	TokenOpenBrace
	TokenCloseBrace
	TokenCDO
	TokenCDC
)

// backwards compatibility
const TokenChar = TokenDelim

// tokenNames maps tokenType's to their names.  Used for conversion to string.
var tokenNames = map[TokenType]string{
	TokenError:          "error",
	TokenEOF:            "EOF",
	TokenIdent:          "IDENT",
	TokenAtKeyword:      "ATKEYWORD",
	TokenString:         "STRING",
	TokenHash:           "HASH",
	TokenNumber:         "NUMBER",
	TokenPercentage:     "PERCENTAGE",
	TokenDimension:      "DIMENSION",
	TokenURI:            "URI",
	TokenUnicodeRange:   "UNICODE-RANGE",
	TokenCDO:            "CDO",
	TokenCDC:            "CDC",
	TokenS:              "S",
	TokenComment:        "COMMENT",
	TokenFunction:       "FUNCTION",
	TokenIncludes:       "INCLUDES",
	TokenDashMatch:      "DASHMATCH",
	TokenPrefixMatch:    "PREFIXMATCH",
	TokenSuffixMatch:    "SUFFIXMATCH",
	TokenSubstringMatch: "SUBSTRINGMATCH",
	TokenDelim:          "DELIM",
	TokenBadString:      "BAD-STRING",
	TokenBadURI:         "BAD-URI",
	TokenBadEscape:      "BAD-ESCAPE",
	TokenColumn:         "COLUMN",
	TokenColon:          "COLON",
	TokenSemicolon:      "SEMICOLON",
	TokenComma:          "COMMA",
	TokenOpenBracket:    "LEFT-BRACKET", // []
	TokenCloseBracket:   "RIGHT-BRACKET",
	TokenOpenParen:      "LEFT-PAREN", // ()
	TokenCloseParen:     "RIGHT-PAREN",
	TokenOpenBrace:      "LEFT-BRACE", // {}
	TokenCloseBrace:     "RIGHT-BRACE",
}

// TokenExtra fills the .Extra field of a token.  Consumers should perform a
// type cast to the proper type to inspect its data.
type TokenExtra interface {
	String() string
}

// TokenExtraTypeLookup provides a handy check for whether a given token type
// should contain extra data.
var TokenExtraTypeLookup = map[TokenType]TokenExtra{
	TokenError:        &TokenExtraError{},
	TokenBadEscape:    &TokenExtraError{},
	TokenBadString:    &TokenExtraError{},
	TokenBadURI:       &TokenExtraError{},
	TokenHash:         &TokenExtraHash{},
	TokenNumber:       &TokenExtraNumeric{},
	TokenPercentage:   &TokenExtraNumeric{},
	TokenDimension:    &TokenExtraNumeric{},
	TokenUnicodeRange: &TokenExtraUnicodeRange{},
}

// TokenExtraHash is attached to TokenHash.
type TokenExtraHash struct {
	IsIdentifier bool
}

// Returns a descriptive string, either "unrestricted" or "id".
func (e *TokenExtraHash) String() string {
	if e == nil || !e.IsIdentifier {
		return "unrestricted"
	} else {
		return "id"
	}
}

// TokenExtraNumeric is attached to TokenNumber, TokenPercentage, and
// TokenDimension.
type TokenExtraNumeric struct {
	// Value float64 // omitted from this implementation
	NonInteger bool
	Dimension  string
}

// Returns the Dimension field.
func (e *TokenExtraNumeric) String() string {
	if e == nil {
		return ""
	}
	return e.Dimension
}

// TokenExtraUnicodeRange is attached to a TokenUnicodeRange.
type TokenExtraUnicodeRange struct {
	Start rune
	End   rune
}

// Returns a valid CSS representation of the token.
func (e *TokenExtraUnicodeRange) String() string {
	if e == nil {
		panic("TokenExtraUnicodeRange: unexpected nil pointer value")
	}

	if e.Start == e.End {
		return fmt.Sprintf("U+%04X", e.Start)
	} else {
		return fmt.Sprintf("U+%04X-%04X", e.Start, e.End)
	}
}

// TokenExtraError is attached to a TokenError and contains the same value as
// Tokenizer.Err(). See also the ParseError type and ParseError.Recoverable().
type TokenExtraError struct {
	Err error
}

// Returns Err.Error().
func (e *TokenExtraError) String() string {
	return e.Err.Error()
}

// Error implements error.
func (e *TokenExtraError) Error() string {
	return e.Err.Error()
}

// Cause implements errors.Causer.
func (e *TokenExtraError) Cause() error {
	return e.Err
}

// Returns the ParseError object, if present.
func (e *TokenExtraError) ParseError() *ParseError {
	pe, ok := e.Err.(*ParseError)
	if !ok {
		return nil
	}
	return pe
}

func escapeIdentifier(s string) string { return escapeIdent(s, 0) }
func escapeHashName(s string) string   { return escapeIdent(s, 1) }
func escapeDimension(s string) string  { return escapeIdent(s, 2) }

func needsHexEscaping(c byte, mode int) bool {
	if c < 0x20 {
		return true
	}
	if c >= utf8.RuneSelf {
		return false
	}
	if mode == 2 {
		if c == 'e' || c == 'E' {
			return true
		}
	}
	if c == '\\' {
		return true
	}
	if isNameCode(c) {
		return false
	}
	return true
}

func escapeIdent(s string, mode int) string {
	if s == "" {
		return ""
	}
	var buf bytes.Buffer
	buf.Grow(len(s))
	anyChanges := false

	var i int

	// Handle first character
	// dashes allowed at start only for TokenIdent-ish
	// eE not allowed at start for Dimension
	if mode != 1 {
		if !isNameStart(s[0]) && s[0] != '-' && s[0] != 'e' && s[0] != 'E' {
			if needsHexEscaping(s[0], mode) {
				fmt.Fprintf(&buf, "\\%X ", s[0])
				anyChanges = true
			} else {
				buf.WriteByte('\\')
				buf.WriteByte(s[0])
				anyChanges = true
			}
		} else if s[0] == 'e' || s[0] == 'E' {
			if mode == 2 {
				fmt.Fprintf(&buf, "\\%X ", s[0])
				anyChanges = true
			} else {
				buf.WriteByte(s[0])
			}
		} else if s[0] == '-' {
			if len(s) == 1 {
				return "\\-"
			} else if isNameStart(s[1]) {
				buf.WriteByte('-')
			} else {
				buf.WriteString("\\-")
				anyChanges = true
			}
		} else {
			buf.WriteByte(s[0])
		}
		i = 1
	} else {
		i = 0
	}
	// Write the rest of the name
	for ; i < len(s); i++ {
		if !isNameCode(s[i]) {
			fmt.Fprintf(&buf, "\\%X ", s[i])
			anyChanges = true
		} else {
			buf.WriteByte(s[i])
		}
	}

	if !anyChanges {
		return s
	}
	return buf.String()
}

func escapeString(s string, delim byte) string {
	var buf bytes.Buffer
	if delim != 0 {
		buf.WriteByte(delim)
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			buf.WriteString("\\\"")
			continue
		case delim:
			buf.WriteByte('\\')
			buf.WriteByte(delim)
			continue
		case '\n':
			buf.WriteString("\\0A ")
			continue
		case '\r':
			buf.WriteString("\\0D ")
			continue
		case '\\':
			buf.WriteString("\\\\")
			continue
		}
		if s[i] < utf8.RuneSelf && isNonPrintable(s[i]) {
			fmt.Fprintf(&buf, "\\%X ", s[i])
			continue
		}
		buf.WriteByte(s[i])
	}
	if delim != 0 {
		buf.WriteByte(delim)
	}
	return buf.String()
}

// Return the CSS source representation of the token.  (Wrapper around
// WriteTo.)
func (t *Token) Render() string {
	var buf bytes.Buffer
	_, _ = t.WriteTo(&buf)
	return buf.String()
}

func stickyWriteString(n *int64, err *error, w io.Writer, s string) {
	n2, err2 := io.WriteString(w, s)
	*n += int64(n2)
	if err2 != nil {
		if *err != nil {
			*err = err2
		}
	}
}

// Write the CSS source representation of the token to the provided writer.  If
// you are attempting to render a series of tokens, see the TokenRenderer type
// to handle comment insertion rules.
//
// Tokens with type TokenError do not write anything.
func (t *Token) WriteTo(w io.Writer) (n int64, err error) {
	switch t.Type {
	case TokenError:
		return
	case TokenEOF:
		return
	case TokenIdent:
		stickyWriteString(&n, &err, w, escapeIdentifier(t.Value))
		return
	case TokenAtKeyword:
		stickyWriteString(&n, &err, w, "@")
		stickyWriteString(&n, &err, w, escapeIdentifier(t.Value))
		return
	case TokenDelim:
		if t.Value == "\\" {
			// nb: should not happen, this is actually TokenBadEscape
			stickyWriteString(&n, &err, w, "\\\n")
		} else {
			stickyWriteString(&n, &err, w, t.Value)
		}
		return
	case TokenHash:
		e := t.Extra.(*TokenExtraHash)
		stickyWriteString(&n, &err, w, "#")
		if e.IsIdentifier {
			stickyWriteString(&n, &err, w, escapeIdentifier(t.Value))
		} else {
			stickyWriteString(&n, &err, w, escapeHashName(t.Value))
		}
		return
	case TokenPercentage:
		stickyWriteString(&n, &err, w, t.Value)
		stickyWriteString(&n, &err, w, "%")
		return
	case TokenDimension:
		e := t.Extra.(*TokenExtraNumeric)
		stickyWriteString(&n, &err, w, t.Value)
		stickyWriteString(&n, &err, w, escapeDimension(e.Dimension))
		return
	case TokenString:
		stickyWriteString(&n, &err, w, escapeString(t.Value, '"'))
		return
	case TokenURI:
		stickyWriteString(&n, &err, w, "url(")
		stickyWriteString(&n, &err, w, escapeString(t.Value, '"'))
		stickyWriteString(&n, &err, w, ")")
		return
	case TokenUnicodeRange:
		stickyWriteString(&n, &err, w, t.Extra.String())
		return
	case TokenComment:
		stickyWriteString(&n, &err, w, "/*")
		stickyWriteString(&n, &err, w, t.Value)
		stickyWriteString(&n, &err, w, "*/")
		return
	case TokenFunction:
		stickyWriteString(&n, &err, w, escapeIdentifier(t.Value))
		stickyWriteString(&n, &err, w, "(")
		return
	case TokenBadEscape:
		stickyWriteString(&n, &err, w, "\\\n")
		return
	case TokenBadString:
		stickyWriteString(&n, &err, w, "\"")
		stickyWriteString(&n, &err, w, escapeString(t.Value, 0))
		stickyWriteString(&n, &err, w, "\n")
		return
	case TokenBadURI:
		stickyWriteString(&n, &err, w, "url(\"")
		str := escapeString(t.Value, 0)
		str = strings.TrimSuffix(str, "\"")
		stickyWriteString(&n, &err, w, str)
		stickyWriteString(&n, &err, w, "\n)")
		return
	default:
		stickyWriteString(&n, &err, w, t.Value)
		return
	}
}

// TokenRenderer takes care of the comment insertion rules for serialization.
// This type is mostly intended for the fuzz test and not for general
// consumption, but it can be used by consumers that want to re-render a parse
// stream.
type TokenRenderer struct {
	lastToken Token
}

// Write a token to the given io.Writer, potentially inserting an empty comment
// in front based on what the previous token was.
func (r *TokenRenderer) WriteTokenTo(w io.Writer, t Token) (n int64, err error) {
	var prevKey, curKey interface{}
	if r.lastToken.Type == TokenDelim {
		prevKey = r.lastToken.Value[0]
	} else {
		prevKey = r.lastToken.Type
	}
	if t.Type == TokenDelim {
		curKey = t.Value[0]
	} else {
		curKey = t.Type
	}

	m1, ok := commentInsertionRules[prevKey]
	if ok {
		if m1[curKey] {
			stickyWriteString(&n, &err, w, "/**/")
		}
	}

	n2, err2 := t.WriteTo(w)
	r.lastToken = t

	n += n2
	if err2 != nil && err == nil {
		err = err2
	}
	return n, err
}

// CSS Syntax Level 3 - Section 9

var commentInsertionThruCDC = map[interface{}]bool{
	TokenIdent:        true,
	TokenFunction:     true,
	TokenURI:          true,
	TokenBadURI:       true,
	TokenNumber:       true,
	TokenPercentage:   true,
	TokenDimension:    true,
	TokenUnicodeRange: true,
	TokenCDC:          true,
	'-':               true,
	'(':               false,
}

var commentInsertionRules = map[interface{}]map[interface{}]bool{
	TokenIdent: map[interface{}]bool{
		TokenIdent:        true,
		TokenFunction:     true,
		TokenURI:          true,
		TokenBadURI:       true,
		'-':               true,
		TokenNumber:       true,
		TokenPercentage:   true,
		TokenDimension:    true,
		TokenUnicodeRange: true,
		TokenCDC:          true,
		'(':               true,
	},
	TokenAtKeyword: commentInsertionThruCDC,
	TokenHash:      commentInsertionThruCDC,
	TokenDimension: commentInsertionThruCDC,
	'#': map[interface{}]bool{
		TokenIdent:        true,
		TokenFunction:     true,
		TokenURI:          true,
		TokenBadURI:       true,
		TokenNumber:       true,
		TokenPercentage:   true,
		TokenDimension:    true,
		TokenUnicodeRange: true,
		TokenCDC:          false,
		'-':               true,
		'(':               false,
	},
	'-': map[interface{}]bool{
		TokenIdent:        true,
		TokenFunction:     true,
		TokenURI:          true,
		TokenBadURI:       true,
		TokenNumber:       true,
		TokenPercentage:   true,
		TokenDimension:    true,
		TokenUnicodeRange: true,
		TokenCDC:          false,
		'-':               false,
		'(':               false,
	},
	TokenNumber: map[interface{}]bool{
		TokenIdent:        true,
		TokenFunction:     true,
		TokenURI:          true,
		TokenBadURI:       true,
		TokenNumber:       true,
		TokenPercentage:   true,
		TokenDimension:    true,
		TokenUnicodeRange: true,
		TokenCDC:          false,
		'-':               false,
		'(':               false,
	},
	'@': map[interface{}]bool{
		TokenIdent:        true,
		TokenFunction:     true,
		TokenURI:          true,
		TokenBadURI:       true,
		TokenNumber:       false,
		TokenPercentage:   false,
		TokenDimension:    false,
		TokenUnicodeRange: true,
		TokenCDC:          false,
		'-':               true,
		'(':               false,
	},
	TokenUnicodeRange: map[interface{}]bool{
		TokenIdent:        true,
		TokenFunction:     true,
		TokenNumber:       true,
		TokenPercentage:   true,
		TokenDimension:    true,
		TokenUnicodeRange: false,
		'?':               true,
	},
	'.': map[interface{}]bool{
		TokenNumber:     true,
		TokenPercentage: true,
		TokenDimension:  true,
	},
	'+': map[interface{}]bool{
		TokenNumber:     true,
		TokenPercentage: true,
		TokenDimension:  true,
	},
	'$': map[interface{}]bool{
		'=': true,
	},
	'*': map[interface{}]bool{
		'=': true,
	},
	'^': map[interface{}]bool{
		'=': true,
	},
	'~': map[interface{}]bool{
		'=': true,
	},
	'|': map[interface{}]bool{
		'=': true,
		'|': true,
	},
	'/': map[interface{}]bool{
		'*': true,
	},
}
