package javare

import (
	"strings"
	"unicode"
)

// Character classes, mirroring java.util.regex.CharPredicates.

var (
	asciiDigit  = csRange('0', '9')
	asciiAlpha  = csRange('a', 'z').union(csRange('A', 'Z'))
	asciiAlnum  = asciiAlpha.union(asciiDigit)
	asciiWord   = asciiAlnum.union(csRune('_'))
	asciiSpace  = csRunes(' ', '\t', '\n', 0x0B, '\f', '\r')
	asciiPunct  = csRange('!', '/').union(csRange(':', '@')).union(csRange('[', '`')).union(csRange('{', '~'))
	asciiGraph  = asciiAlnum.union(asciiPunct)
	asciiXDigit = asciiDigit.union(csRange('a', 'f')).union(csRange('A', 'F'))
	asciiCntrl  = csRange(0, 0x1F).union(csRune(0x7F))
	hSpace      = csRunes(' ', '\t', 0xA0, 0x1680, 0x180E, 0x202F, 0x205F, 0x3000).union(csRange(0x2000, 0x200A))
	vSpace      = csRange('\n', '\r').union(csRunes(0x85, 0x2028, 0x2029))
	lineTerms   = csRunes('\n', '\r', 0x85, 0x2028, 0x2029)
)

func cat(ts ...*unicode.RangeTable) charset { return csFromTables(ts...) }

func unassigned() charset {
	var all charset
	for _, t := range unicode.Categories {
		all = all.union(csFromTable(t))
	}
	return all.negate()
}

func alphabetic() charset { return cat(unicode.L, unicode.Nl, unicode.Other_Alphabetic) }
func lowercase() charset  { return cat(unicode.Ll, unicode.Other_Lowercase) }
func uppercase() charset  { return cat(unicode.Lu, unicode.Other_Uppercase) }
func titlecase() charset  { return cat(unicode.Lt) }
func digit() charset      { return cat(unicode.Nd) }
func control() charset    { return cat(unicode.Cc) }
func punctuation() charset {
	return cat(unicode.Pc, unicode.Pd, unicode.Ps, unicode.Pe, unicode.Pi, unicode.Pf, unicode.Po)
}

func whiteSpace() charset {
	return cat(unicode.Zs, unicode.Zl, unicode.Zp).union(csRange(9, 13)).union(csRune(0x85))
}

func hexDigit() charset {
	return digit().union(asciiXDigit).union(csRange(0xFF10, 0xFF19)).union(csRange(0xFF21, 0xFF26)).union(csRange(0xFF41, 0xFF46))
}

func blank() charset { return cat(unicode.Zs).union(csRune('\t')) }

func graph() charset {
	return unassigned().union(cat(unicode.Zs, unicode.Zl, unicode.Zp, unicode.Cc, unicode.Cs)).negate()
}

func printable() charset { return graph().union(blank()).intersect(control().negate()) }

func joinControl() charset { return csRunes(0x200C, 0x200D) }

func word() charset {
	return alphabetic().union(cat(unicode.Mn, unicode.Me, unicode.Mc, unicode.Nd, unicode.Pc)).union(joinControl())
}

func anyCase() charset { return lowercase().union(uppercase()).union(titlecase()) }

func wordSet(f Flag) charset {
	if f&UnicodeCharacterClass != 0 {
		return word()
	}
	return asciiWord
}

func digitSet(f Flag) charset {
	if f&UnicodeCharacterClass != 0 {
		return digit()
	}
	return asciiDigit
}

func spaceSet(f Flag) charset {
	if f&UnicodeCharacterClass != 0 {
		return whiteSpace()
	}
	return asciiSpace
}

// getUnicodePredicate: binary Unicode properties (\p{IsAlphabetic}, ...).
func getUnicodePredicate(name string, ci bool) (charset, bool) {
	switch name {
	case "ALPHABETIC":
		return alphabetic(), true
	case "ASSIGNED":
		return unassigned().negate(), true
	case "CONTROL":
		return control(), true
	case "HEXDIGIT", "HEX_DIGIT":
		return hexDigit(), true
	case "IDEOGRAPHIC":
		return cat(unicode.Ideographic), true
	case "JOINCONTROL", "JOIN_CONTROL":
		return joinControl(), true
	case "LETTER":
		return cat(unicode.L), true
	case "LOWERCASE":
		if ci {
			return anyCase(), true
		}
		return lowercase(), true
	case "NONCHARACTERCODEPOINT", "NONCHARACTER_CODE_POINT":
		return cat(unicode.Noncharacter_Code_Point), true
	case "TITLECASE":
		if ci {
			return anyCase(), true
		}
		return titlecase(), true
	case "PUNCTUATION":
		return punctuation(), true
	case "UPPERCASE":
		if ci {
			return anyCase(), true
		}
		return uppercase(), true
	case "WHITESPACE", "WHITE_SPACE":
		return whiteSpace(), true
	case "WORD":
		return word(), true
	}
	// EMOJI, EMOJI_PRESENTATION, ... need tables Go's unicode package lacks.
	return nil, false
}

// getPosixPredicate: Unicode POSIX classes (\p{IsAlpha}, or \p{Alpha} under
// UNICODE_CHARACTER_CLASS).
func getPosixPredicate(name string, ci bool) (charset, bool) {
	switch name {
	case "ALPHA":
		return alphabetic(), true
	case "LOWER":
		if ci {
			return anyCase(), true
		}
		return lowercase(), true
	case "UPPER":
		if ci {
			return anyCase(), true
		}
		return uppercase(), true
	case "SPACE":
		return whiteSpace(), true
	case "PUNCT":
		return punctuation(), true
	case "XDIGIT":
		return hexDigit(), true
	case "ALNUM":
		return alphabetic().union(digit()), true
	case "CNTRL":
		return control(), true
	case "DIGIT":
		return digit(), true
	case "BLANK":
		return blank(), true
	case "GRAPH":
		return graph(), true
	case "PRINT":
		return printable(), true
	}
	return nil, false
}

func forUnicodeProperty(name string, ci bool) (charset, bool) {
	upper := strings.ToUpper(name)
	if cs, ok := getUnicodePredicate(upper, ci); ok {
		return cs, true
	}
	return getPosixPredicate(upper, ci)
}

var generalCategories = map[string][]*unicode.RangeTable{
	"Lm": {unicode.Lm}, "Lo": {unicode.Lo}, "Mn": {unicode.Mn}, "Me": {unicode.Me}, "Mc": {unicode.Mc},
	"Nd": {unicode.Nd}, "Nl": {unicode.Nl}, "No": {unicode.No}, "Zs": {unicode.Zs}, "Zl": {unicode.Zl},
	"Zp": {unicode.Zp}, "Cc": {unicode.Cc}, "Cf": {unicode.Cf}, "Co": {unicode.Co}, "Cs": {unicode.Cs},
	"Pd": {unicode.Pd}, "Ps": {unicode.Ps}, "Pe": {unicode.Pe}, "Pc": {unicode.Pc}, "Po": {unicode.Po},
	"Sm": {unicode.Sm}, "Sc": {unicode.Sc}, "Sk": {unicode.Sk}, "So": {unicode.So}, "Pi": {unicode.Pi},
	"Pf": {unicode.Pf}, "L": {unicode.L}, "M": {unicode.M}, "N": {unicode.N}, "Z": {unicode.Z},
	"P": {unicode.P}, "S": {unicode.S}, "LD": {unicode.L, unicode.Nd},
}

// forProperty: category names and the ASCII POSIX/java.lang.Character
// classes (\p{Lu}, \p{Alpha}, \p{javaLowerCase}, ...).
func forProperty(name string, ci bool) (charset, bool) {
	if ts, ok := generalCategories[name]; ok {
		return cat(ts...), true
	}
	switch name {
	case "Cn":
		return unassigned(), true
	case "Lu":
		if ci {
			return cat(unicode.Lu, unicode.Ll, unicode.Lt), true
		}
		return cat(unicode.Lu), true
	case "Ll":
		if ci {
			return cat(unicode.Lu, unicode.Ll, unicode.Lt), true
		}
		return cat(unicode.Ll), true
	case "Lt":
		if ci {
			return cat(unicode.Lu, unicode.Ll, unicode.Lt), true
		}
		return cat(unicode.Lt), true
	case "C":
		return unassigned().union(cat(unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)), true
	case "LC":
		return cat(unicode.Lu, unicode.Ll, unicode.Lt), true
	case "L1":
		return csRange(0, 255), true
	case "all":
		return csAll, true
	case "ASCII":
		return csRange(0, 127), true
	case "Alnum":
		return asciiAlnum, true
	case "Alpha":
		return asciiAlpha, true
	case "Blank":
		return csRunes(' ', '\t'), true
	case "Cntrl":
		return asciiCntrl, true
	case "Digit":
		return asciiDigit, true
	case "Graph":
		return asciiGraph, true
	case "Lower":
		if ci {
			return asciiAlpha, true
		}
		return csRange('a', 'z'), true
	case "Print":
		return csRange(32, 126), true
	case "Punct":
		return asciiPunct, true
	case "Space":
		return asciiSpace, true
	case "Upper":
		if ci {
			return asciiAlpha, true
		}
		return csRange('A', 'Z'), true
	case "XDigit":
		return asciiXDigit, true
	case "javaLowerCase":
		if ci {
			return anyCase(), true
		}
		return lowercase(), true
	case "javaUpperCase":
		if ci {
			return anyCase(), true
		}
		return uppercase(), true
	case "javaTitleCase":
		if ci {
			return anyCase(), true
		}
		return titlecase(), true
	case "javaAlphabetic":
		return alphabetic(), true
	case "javaIdeographic":
		return cat(unicode.Ideographic), true
	case "javaDigit":
		return digit(), true
	case "javaDefined":
		return unassigned().negate(), true
	case "javaLetter":
		return cat(unicode.L), true
	case "javaLetterOrDigit":
		return cat(unicode.L, unicode.Nd), true
	case "javaJavaIdentifierStart":
		return cat(unicode.L, unicode.Nl, unicode.Sc, unicode.Pc), true
	case "javaJavaIdentifierPart":
		return cat(unicode.L, unicode.Nl, unicode.Sc, unicode.Pc, unicode.Nd, unicode.Mn, unicode.Mc).union(identifierIgnorable()), true
	case "javaUnicodeIdentifierStart":
		return cat(unicode.L, unicode.Nl, unicode.Other_ID_Start), true
	case "javaUnicodeIdentifierPart":
		return cat(unicode.L, unicode.Nl, unicode.Other_ID_Start, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc,
			unicode.Other_ID_Continue).union(identifierIgnorable()), true
	case "javaIdentifierIgnorable":
		return identifierIgnorable(), true
	case "javaSpaceChar":
		return cat(unicode.Zs, unicode.Zl, unicode.Zp), true
	case "javaWhitespace":
		ws := cat(unicode.Zs, unicode.Zl, unicode.Zp).intersect(csRunes(0xA0, 0x2007, 0x202F).negate())
		return ws.union(csRange(0x09, 0x0D)).union(csRange(0x1C, 0x1F)), true
	case "javaISOControl":
		return csRange(0, 0x1F).union(csRange(0x7F, 0x9F)), true
	case "javaMirrored":
		// Go has no Bidi_Mirrored table; the common paired brackets only.
		return csRunes('(', ')', '<', '>', '[', ']', '{', '}', 0xAB, 0xBB, 0x2039, 0x203A, 0x2045, 0x2046,
			0x207D, 0x207E, 0x208D, 0x208E, 0x3008, 0x3009, 0x300A, 0x300B, 0x300C, 0x300D, 0x300E, 0x300F,
			0x3010, 0x3011), true
	}
	return nil, false
}

func identifierIgnorable() charset {
	return csRange(0, 8).union(csRange(0x0E, 0x1B)).union(csRange(0x7F, 0x9F)).union(cat(unicode.Cf))
}

// script is Character.UnicodeScript.forName (full names, any case).
func script(name string) (charset, bool) {
	for k, t := range unicode.Scripts {
		if strings.EqualFold(k, name) {
			return csFromTable(t), true
		}
	}
	return nil, false
}

// blocks lists common Unicode blocks (Java's \p{InXxx}); names are matched
// ignoring case, spaces, underscores and hyphens.
var blocks = []struct {
	name   string
	lo, hi rune
}{
	{"Basic Latin", 0x0000, 0x007F},
	{"Latin-1 Supplement", 0x0080, 0x00FF},
	{"Latin Extended-A", 0x0100, 0x017F},
	{"Latin Extended-B", 0x0180, 0x024F},
	{"IPA Extensions", 0x0250, 0x02AF},
	{"Spacing Modifier Letters", 0x02B0, 0x02FF},
	{"Combining Diacritical Marks", 0x0300, 0x036F},
	{"Greek and Coptic", 0x0370, 0x03FF},
	{"Greek", 0x0370, 0x03FF},
	{"Cyrillic", 0x0400, 0x04FF},
	{"Armenian", 0x0530, 0x058F},
	{"Hebrew", 0x0590, 0x05FF},
	{"Arabic", 0x0600, 0x06FF},
	{"Devanagari", 0x0900, 0x097F},
	{"Thai", 0x0E00, 0x0E7F},
	{"Hangul Jamo", 0x1100, 0x11FF},
	{"Latin Extended Additional", 0x1E00, 0x1EFF},
	{"Greek Extended", 0x1F00, 0x1FFF},
	{"General Punctuation", 0x2000, 0x206F},
	{"Superscripts and Subscripts", 0x2070, 0x209F},
	{"Currency Symbols", 0x20A0, 0x20CF},
	{"Letterlike Symbols", 0x2100, 0x214F},
	{"Number Forms", 0x2150, 0x218F},
	{"Arrows", 0x2190, 0x21FF},
	{"Mathematical Operators", 0x2200, 0x22FF},
	{"Miscellaneous Technical", 0x2300, 0x23FF},
	{"Box Drawing", 0x2500, 0x257F},
	{"Block Elements", 0x2580, 0x259F},
	{"Geometric Shapes", 0x25A0, 0x25FF},
	{"Miscellaneous Symbols", 0x2600, 0x26FF},
	{"Dingbats", 0x2700, 0x27BF},
	{"CJK Symbols and Punctuation", 0x3000, 0x303F},
	{"Hiragana", 0x3040, 0x309F},
	{"Katakana", 0x30A0, 0x30FF},
	{"CJK Unified Ideographs", 0x4E00, 0x9FFF},
	{"Hangul Syllables", 0xAC00, 0xD7AF},
	{"Private Use Area", 0xE000, 0xF8FF},
	{"Alphabetic Presentation Forms", 0xFB00, 0xFB4F},
	{"Halfwidth and Fullwidth Forms", 0xFF00, 0xFFEF},
	{"Specials", 0xFFF0, 0xFFFF},
	{"Miscellaneous Symbols and Pictographs", 0x1F300, 0x1F5FF},
	{"Emoticons", 0x1F600, 0x1F64F},
	{"Transport and Map Symbols", 0x1F680, 0x1F6FF},
	{"Supplemental Symbols and Pictographs", 0x1F900, 0x1F9FF},
}

func block(name string) (charset, bool) {
	norm := func(s string) string {
		return strings.ToLower(strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s))
	}
	want := norm(name)
	for _, b := range blocks {
		if norm(b.name) == want {
			return csRange(b.lo, b.hi), true
		}
	}
	return nil, false
}

// lookupProperty resolves the name of \p{name} exactly as Pattern.family
// does.
func (p *jparser) lookupProperty(name string) (charset, bool) {
	ci := p.has(CaseInsensitive)
	if i := strings.IndexByte(name, '='); i >= 0 {
		value := name[i+1:]
		switch strings.ToLower(name[:i]) {
		case "sc", "script":
			return script(value)
		case "blk", "block":
			return block(value)
		case "gc", "general_category":
			return forProperty(value, ci)
		}
		return nil, false
	}
	if strings.HasPrefix(name, "In") {
		return block(name[2:])
	}
	if strings.HasPrefix(name, "Is") {
		short := name[2:]
		if cs, ok := forUnicodeProperty(short, ci); ok {
			return cs, true
		}
		if cs, ok := forProperty(short, ci); ok {
			return cs, true
		}
		return script(short)
	}
	if p.has(UnicodeCharacterClass) {
		if cs, ok := getPosixPredicate(strings.ToUpper(name), ci); ok {
			return cs, true
		}
	}
	return forProperty(name, ci)
}

// runeByName would implement \N{name}; Unicode character names are not
// available without large tables, so it always fails.
func runeByName(string) (rune, bool) { return 0, false }
