TestDekenJSON : UnitTest {
	test_stringifyNested {
		this.assertEquals(
			DekenJSON.stringify((b: [1, 2.5, true, nil], a: "x\"y")),
			"{\"a\":\"x\\\"y\",\"b\":[1,2.5,true,null]}"
		)
	}

	test_roundTrip {
		var o = (name: "fmx6", n: 3, ok: true);
		this.assertEquals(DekenJSON.parse(DekenJSON.stringify(o))["n"], 3)
	}

	test_parseBool {
		this.assertEquals(DekenJSON.parse("{\"ok\":false}")["ok"], false)
	}

	test_parseKeepsNumberLookingStrings {
		var o = DekenJSON.parse("{\"sha\":\"1234567\",\"v\":\"true\"}");
		this.assertEquals(o["sha"], "1234567");
		this.assertEquals(o["v"], "true")
	}

	test_parseNumbersAndNull {
		var o = DekenJSON.parse("{\"i\":-3,\"f\":2.5,\"e\":1e2,\"n\":null,\"a\":[1,[]]}");
		this.assertEquals(o["i"], -3);
		this.assertEquals(o["f"], 2.5);
		this.assertEquals(o["e"], 100.0);
		this.assertEquals(o["n"], nil);
		this.assertEquals(o["a"], [1, []])
	}

	test_parseEscapes {
		// JSON text: "a\n\t\"\\\u00e9" -> a, newline, tab, quote, backslash, e-acute (2 UTF-8 bytes) = 7 chars
		this.assertEquals(DekenJSON.parse("\"a\\n\\t\\\"\\\\\\u00e9\"").size, 7)
	}

	test_stringifyEscapesControlChars {
		this.assertEquals(DekenJSON.stringify("a\nb" ++ 1.asAscii), "\"a\\nb\\u0001\"")
	}

	test_stringifyNonFiniteIsNull {
		this.assertEquals(DekenJSON.stringify([inf, 0/0, -inf]), "[null,null,null]")
	}

	test_stringifySymbolAndNonStringKeys {
		this.assertEquals(DekenJSON.stringify(Dictionary[2 -> \x, 1 -> 'y z']), "{\"1\":\"y z\",\"2\":\"x\"}")
	}

	test_parseRejectsGarbage {
		this.assertException({ DekenJSON.parse("{\"a\":}") }, Error, "garbage should throw")
	}
	test_stringifyKeepsNonAscii {
		this.assertEquals(DekenJSON.stringify("é"), "\"é\"")
	}

	test_roundTripNonAscii {
		var s = "é ü 😀 C:/Users/Stéfaan";
		this.assertEquals(DekenJSON.parse(DekenJSON.stringify([s]))[0], s)
	}

	test_parseRejectsMalformedNumbers {
		["-", "1.2.3", "--5", "1e", "01", "1.", "+1"].do { |text|
			this.assertException({ DekenJSON.parse(text) }, Error, "should reject " ++ text)
		}
	}
}
