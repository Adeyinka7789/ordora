package render

import "testing"

// TestAllTemplatesParse guards against template syntax errors (including
// unknown template functions, which fail at parse time). The server only
// parses templates at startup, so without this test a typo would slip
// through go build and explode at runtime.
func TestAllTemplatesParse(t *testing.T) {
	r, err := New("../templates")
	if err != nil {
		t.Fatalf("templates parse: %v", err)
	}
	for _, name := range []string{
		"layouts/app.html",
		"layouts/admin.html",
		"notifications/index.html",
		"support/index.html",
		"support/show.html",
		"admin/complaints_list.html",
		"admin/complaint_detail.html",
		"admin/broadcasts.html",
		"payments/index.html",
		"payments/_table.html",
	} {
		if r.tmpl.Lookup(name) == nil {
			t.Errorf("missing template %s", name)
		}
	}
}
