package render

import (
	"testing"
	"text/template"
)

func TestTemplateFuncs_HasDict(t *testing.T) {
	fm := templateFuncs()
	if _, ok := fm["dict"]; !ok {
		t.Fatal("dict is not in templateFuncs()")
	}
	if _, ok := fm["add1"]; !ok {
		t.Fatal("add1 is not in templateFuncs()")
	}
	if _, ok := fm["list"]; !ok {
		t.Fatal("list is not in templateFuncs()")
	}
}

func TestTemplateFuncs_ParsesDictUsage(t *testing.T) {
	tmpl := template.New("").Funcs(templateFuncs())
	_, err := tmpl.New("test").Parse(`{{ $x := dict "a" 1 "b" 2 }}{{ index $x "a" }}`)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
}
