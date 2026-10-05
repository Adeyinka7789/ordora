package measurement

import (
	"testing"

	"github.com/google/uuid"
)

func testTemplate() Template {
	fields, err := ParseFields([]byte(`[
		{"key":"chest","label":"Chest","unit":"in","required":true},
		{"key":"waist","label":"Waist","unit":"in","required":false}
	]`))
	if err != nil {
		panic(err)
	}
	return Template{
		ID:       uuid.New(),
		Gender:   GenderMale,
		Garment:  "shirt",
		Name:     "Men's Shirt",
		Fields:   fields,
		IsSystem: true,
	}
}

func TestParseFields(t *testing.T) {
	if _, err := ParseFields([]byte(`not json`)); err == nil {
		t.Error("invalid JSON should fail")
	}
	if _, err := ParseFields([]byte(`[]`)); err == nil {
		t.Error("empty fields should fail")
	}
	if _, err := ParseFields([]byte(`[{"key":"","label":"X"}]`)); err == nil {
		t.Error("missing key should fail")
	}
	if _, err := ParseFields([]byte(`[{"key":"a","label":""},{"key":"a","label":"B"}]`)); err == nil {
		t.Error("missing label / duplicate keys should fail")
	}
	fields, err := ParseFields([]byte(`[{"key":"chest","label":"Chest","unit":"in","required":true}]`))
	if err != nil {
		t.Fatalf("valid fields rejected: %v", err)
	}
	if len(fields) != 1 || fields[0].Key != "chest" {
		t.Errorf("unexpected parse result: %+v", fields)
	}
}

func TestValidateValues(t *testing.T) {
	tmpl := testTemplate()
	if err := tmpl.ValidateValues(map[string]string{"chest": "40", "waist": ""}); err != nil {
		t.Errorf("valid values rejected: %v", err)
	}
	if err := tmpl.ValidateValues(map[string]string{"waist": "32"}); err == nil {
		t.Error("missing required chest should fail")
	}
	if err := tmpl.ValidateValues(map[string]string{"chest": "40", "collar": "15"}); err == nil {
		t.Error("unknown field should fail")
	}
	long := make([]byte, MaxValueLen+1)
	for i := range long {
		long[i] = '9'
	}
	if err := tmpl.ValidateValues(map[string]string{"chest": string(long)}); err == nil {
		t.Error("overlong value should fail")
	}
}

func TestGender(t *testing.T) {
	if !GenderMale.IsValid() || !GenderFemale.IsValid() || !GenderUnisex.IsValid() {
		t.Error("known genders should be valid")
	}
	if Gender("alien").IsValid() {
		t.Error("unknown gender should be invalid")
	}
	if GenderMale.Label() != "Male" || GenderFemale.Label() != "Female" {
		t.Error("unexpected gender labels")
	}
}
