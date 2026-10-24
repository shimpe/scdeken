package scclass

import (
	"reflect"
	"testing"
)

func TestParseSimple(t *testing.T) {
	got := Parse([]byte("FMX6 : MultiOutUGen { *ar {} }\nFMX6Helper { }"))
	want := []Class{{"FMX6", "MultiOutUGen"}, {"FMX6Helper", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseSkipsExtensions(t *testing.T) {
	if got := Parse([]byte("+ SinOsc { foo {} }")); len(got) != 0 {
		t.Fatalf("got %v, want none", got)
	}
}

func TestParseIgnoresCommentsStrings(t *testing.T) {
	src := "// Fake : UGen {\n/* Nope : UGen { */ X { var s = \"Y : UGen {\"; }"
	got := Parse([]byte(src))
	want := []Class{{"X", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseNestedBraces(t *testing.T) {
	src := "A : B { foo { if(x) { Inner { } } } bar { |a| a.collect { Deep : UGen { } } } }\nC { }"
	got := Parse([]byte(src))
	want := []Class{{"A", "B"}, {"C", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseNestedComments(t *testing.T) {
	src := "/* a /* b */ Hidden : UGen { */ Shown { }"
	got := Parse([]byte(src))
	want := []Class{{"Shown", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseHeaderVariants(t *testing.T) {
	src := "Array[slot] : ArrayedCollection {}\nFoo\n// c\n  : /* x */ Bar\n{ }\nMeta_Foo : Meta_Bar { }"
	got := Parse([]byte(src))
	want := []Class{{"Array", "ArrayedCollection"}, {"Foo", "Bar"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseLiterals(t *testing.T) {
	src := `A { var a = $"; var b = ${; var c = $\n; var d = 'q}\''; var e = "x\"}"; var f = \sym; }
B { }`
	got := Parse([]byte(src))
	want := []Class{{"A", ""}, {"B", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestParseUnbalanced(t *testing.T) {
	for _, src := range []string{"A { foo {", "}} B { }", "/* open", "\"open", "A :", "A[", "$", "+"} {
		Parse([]byte(src)) // must not panic
	}
	got := Parse([]byte("}} B { "))
	if want := []Class{{"B", ""}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSplitTransitive(t *testing.T) {
	classes := []Class{{"B", "A"}, {"A", "UGen"}, {"C", "Object"}, {"A", "UGen"}}
	ugens, others := Split(classes)
	if want := []string{"A", "B"}; !reflect.DeepEqual(ugens, want) {
		t.Fatalf("ugens %v, want %v", ugens, want)
	}
	if want := []string{"C"}; !reflect.DeepEqual(others, want) {
		t.Fatalf("others %v, want %v", others, want)
	}
}

func TestSplitCycleTerminates(t *testing.T) {
	_, others := Split([]Class{{"A", "B"}, {"B", "A"}})
	if want := []string{"A", "B"}; !reflect.DeepEqual(others, want) {
		t.Fatalf("others %v, want %v", others, want)
	}
}
