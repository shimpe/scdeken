// JSON for the Deken GUI <-> scdeken helper conversation.
//
// stringify: nil -> null, Boolean, Integer, Float (inf/nan -> null), String,
//   Symbol, Array/List, Dictionary/Event (keys via asString, sorted so the
//   output is stable).
//
// parse: returns Dictionary (objects), Array, String, Integer, Float,
//   Boolean and nil, always with real types.
//
// Why not String:parseJSON? Verified on SuperCollider 3.14.1 (Windows): it
// exists and returns a Dictionary, but it is YAML-based and loses types:
// numbers and booleans come back as Strings ("1", "2.5", "true", "false",
// "-3e2"), only null becomes nil, and "\uXXXX" escapes lose their backslash
// ("é" -> "u00e9"). Converting such strings back is unsafe, because a
// genuine JSON string like "1234567" (a commit sha) or "true" is
// indistinguishable from a number or boolean. So this class has its own small
// parser. Behaviour of parseJSON on 3.13 was not tested here; as the parser
// below does not use it, it does not matter.
//
// Strings are byte strings in sclang: UTF-8 passes through unchanged, and
// "\uXXXX" escapes are decoded to UTF-8 bytes.
// Integers are 32 bit: integer literals beyond that range parse as Float.
DekenJSON {
	var src, pos;

	*stringify { |obj|
		^case
		{ obj.isNil } { "null" }
		{ obj.isKindOf(Boolean) } { obj.asString }
		{ obj.isKindOf(Integer) } { obj.asString }
		{ obj.isKindOf(Float) } { if(obj.isNaN.not and: { obj.abs != inf }) { obj.asString } { "null" } }
		{ obj.isKindOf(String) } { this.prQuote(obj) }
		{ obj.isKindOf(Symbol) } { this.prQuote(obj.asString) }
		{ obj.isKindOf(Dictionary) } { this.prEncodeObject(obj) }
		{ obj.isKindOf(SequenceableCollection) } { this.prEncodeArray(obj) }
		{ Error("DekenJSON cannot encode a %".format(obj.class)).throw };
	}

	*parse { |string|
		^this.new.prParseDocument(string)
	}

	// ---- stringify helpers

	*prEncodeArray { |list|
		^"[" ++ list.collect { |x| this.stringify(x) }.join(",") ++ "]"
	}

	*prEncodeObject { |dict|
		var pairs = dict.keys.asArray.collect { |k| k.asString -> dict.at(k) }.sort { |a, b| a.key < b.key };
		var members = pairs.collect { |a| this.prQuote(a.key) ++ ":" ++ this.stringify(a.value) };
		^"{" ++ members.join(",") ++ "}"
	}

	*prQuote { |string|
		var out = String.new;
		string.do { |c| out = out ++ this.prEscapeChar(c) };
		^"\"" ++ out ++ "\""
	}

	*prEscapeChar { |c|
		^case
		{ c == $" } { "\\\"" }
		{ c == 92.asAscii } { "\\\\" }
		{ c == Char.nl } { "\\n" }
		{ c == Char.tab } { "\\t" }
		{ c == Char.ret } { "\\r" }
		{ c.ascii >= 0 and: { c.ascii < 32 } } { "\\u00" ++ c.ascii.asHexString(2).toLower }
		{ c.asString };
	}

	// ---- parser

	prParseDocument { |string|
		var value;
		src = string.asString;
		pos = 0;
		value = this.prValue;
		this.prSkipSpace;
		if(pos < src.size) { this.prFail("unexpected text after the value") };
		^value
	}

	prFail { |what|
		Error("JSON error at character %: %".format(pos, what)).throw
	}

	prPeek { ^src[pos] }

	prSkipSpace {
		while { pos < src.size and: { src[pos].isSpace } } { pos = pos + 1 }
	}

	prExpect { |char|
		if(this.prPeek != char) { this.prFail("expected '%'".format(char)) };
		pos = pos + 1;
	}

	prValue {
		var c;
		this.prSkipSpace;
		c = this.prPeek;
		^case
		{ c.isNil } { this.prFail("unexpected end of text") }
		{ c == ${ } { this.prObject }
		{ c == $[ } { this.prArray }
		{ c == $" } { this.prString }
		{ c == $t } { this.prLiteral("true", true) }
		{ c == $f } { this.prLiteral("false", false) }
		{ c == $n } { this.prLiteral("null", nil) }
		{ c == $- or: { c.isDecDigit } } { this.prNumber }
		{ this.prFail("unexpected '%'".format(c)) };
	}

	prLiteral { |word, value|
		if(src.copyRange(pos, pos + word.size - 1) != word) { this.prFail("expected " ++ word) };
		pos = pos + word.size;
		^value
	}

	prNumber {
		var start = pos, token, asFloat;
		while { pos < src.size and: { "+-.eE0123456789".includes(src[pos]) } } { pos = pos + 1 };
		token = src.copyRange(start, pos - 1);
		if("^-?(0|[1-9][0-9]*)([.][0-9]+)?([eE][+-]?[0-9]+)?$".matchRegexp(token).not) { pos = start; this.prFail("malformed number") };
		asFloat = token.asFloat;
		if(token.any { |c| ".eE".includes(c) } or: { asFloat.abs > 2147483647 }) { ^asFloat };
		^token.asInteger
	}

	prString {
		var out = String.new, c;
		this.prExpect($");
		while { (c = this.prPeek).notNil and: { c != $" } } {
			pos = pos + 1;
			out = out ++ if(c == 92.asAscii) { this.prEscape } { c.asString };
		};
		this.prExpect($");
		^out
	}

	prEscape {
		var c = this.prPeek;
		pos = pos + 1;
		^case
		{ c == $n } { Char.nl.asString }
		{ c == $t } { Char.tab.asString }
		{ c == $r } { Char.ret.asString }
		{ c == $b } { 8.asAscii.asString }
		{ c == $f } { 12.asAscii.asString }
		{ c == $u } { this.prUnicode }
		{ "\"\\/".includes(c) } { c.asString }
		{ this.prFail("bad escape") };
	}

	// Reads XXXX after "\u" (and a second \uXXXX for surrogate pairs); UTF-8 out.
	prUnicode {
		var code = this.prHex4;
		if(code >= 0xD800 and: { code < 0xDC00 }) {
			this.prExpect(92.asAscii);
			this.prExpect($u);
			code = 0x10000 + ((code - 0xD800) << 10) + (this.prHex4 - 0xDC00);
		};
		^DekenJSON.prUtf8(code)
	}

	prHex4 {
		var digits = src.copyRange(pos, pos + 3);
		if(digits.size < 4 or: { digits.any { |c| c.isDecDigit.not and: { "abcdefABCDEF".includes(c).not } } }) {
			this.prFail("bad \\u escape")
		};
		pos = pos + 4;
		^digits.toLower.ascii.inject(0) { |acc, a| acc * 16 + (if(a >= 97) { a - 87 } { a - 48 }) }
	}

	*prUtf8 { |code|
		var bytes = case
		{ code < 0x80 } { [code] }
		{ code < 0x800 } { [0xC0 | (code >> 6), 0x80 | (code & 0x3F)] }
		{ code < 0x10000 } { [0xE0 | (code >> 12), 0x80 | ((code >> 6) & 0x3F), 0x80 | (code & 0x3F)] }
		{ [0xF0 | (code >> 18), 0x80 | ((code >> 12) & 0x3F), 0x80 | ((code >> 6) & 0x3F), 0x80 | (code & 0x3F)] };
		^bytes.collect(_.asAscii).as(String)
	}

	prArray {
		var list = List.new;
		this.prExpect($[);
		this.prSkipSpace;
		if(this.prPeek == $]) { pos = pos + 1; ^list.asArray };
		while { true } {
			list.add(this.prValue);
			this.prSkipSpace;
			if(this.prPeek == $,) { pos = pos + 1 } { this.prExpect($]); ^list.asArray };
		}
	}

	prObject {
		var dict = Dictionary.new, key;
		this.prExpect(${);
		this.prSkipSpace;
		if(this.prPeek == $}) { pos = pos + 1; ^dict };
		while { true } {
			this.prSkipSpace;
			key = this.prString;
			this.prSkipSpace;
			this.prExpect($:);
			dict.put(key, this.prValue);
			this.prSkipSpace;
			if(this.prPeek == $,) { pos = pos + 1 } { this.prExpect($}); ^dict };
		}
	}
}
