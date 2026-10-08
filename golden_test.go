package tsunami

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden output files")

// goldenCase runs a script against fixtures. When assert is set, the output
// is checked with an MUnit-style assertion script (payload must equalTo(...));
// when expected is set, the output must match the file byte for byte.
type goldenCase struct {
	name     string
	script   string
	payload  string
	vars     map[string]string // var name -> fixture file
	varExprs map[string]string // var name -> expression evaluated on payload
	attrs    string
	assert   string
	expected string
}

const (
	deliveryScript  = "buildDeliverySearchPayloadSub_Flow.dwl"
	materialsEvent  = "set-event_payload.json"
	materialsData   = "payload.data"
	storesQueryFile = "stores-query.json"
)

func delivery(f string) string  { return filepath.Join("testdata", "mulesoft", "delivery", f) }
func materials(f string) string { return filepath.Join("testdata", "mulesoft", "materials", f) }
func samples(f string) string   { return filepath.Join("testdata", "samples", f) }

// materialsCase checks one material extraction script against its MUnit
// assertion.
func materialsCase(name, script, assert string) goldenCase {
	return goldenCase{
		name:     name,
		script:   materials(script),
		payload:  materials(materialsEvent),
		varExprs: map[string]string{"DATA": materialsData},
		assert:   materials(assert),
	}
}

// deliveryCase checks the delivery search payload script. The MUnit
// assertions in the source project predate the GPSTracking field the script
// now emits, so the fixtures include it.
func deliveryCase(name, payload, query, assert string) goldenCase {
	return goldenCase{
		name:    name,
		script:  delivery(deliveryScript),
		payload: delivery(payload),
		vars:    map[string]string{"queryParams": delivery(query)},
		assert:  delivery(assert),
	}
}

func goldenCases() []goldenCase {
	return []goldenCase{
		deliveryCase("delivery search payload", "set-event_payload.dwl", "queryParams.dwl", "assert_expression_payload.dwl"),
		deliveryCase("delivery search payload when empty", "set-event_payload_empty_data.dwl", "queryParams.dwl", "assert_expression_payload_when_empty.dwl"),
		deliveryCase("delivery search payload default properties", "set-event_payload.dwl", "queryParams_without_lms_properties.dwl", "assert_expression_payload_default_lms_properties.dwl"),
		{
			name:    "materials",
			script:  materials("extract-materials.dwl"),
			payload: materials(materialsEvent),
			assert:  materials("assert_expression_materials_payload.dwl"),
		},
		materialsCase("materials classification", "extract-classification.dwl", "assert_expression_classification_payload.dwl"),
		materialsCase("materials sales", "extract-sales.dwl", "assert_expression_sales_payload.dwl"),
		materialsCase("materials plant", "extract-plant.dwl", "assert_expression_plant_payload.dwl"),
		materialsCase("materials purchase", "extract-purchase.dwl", "assert_expression_purchase_payload.dwl"),
		{
			name:     "csv employees to json",
			script:   filepath.Join("example", "basic-json.tsu"),
			payload:  filepath.Join("example", "basic-json.csv"),
			expected: samples("employees.json"),
		},
		{
			name:     "order tracking output builder",
			script:   samples("orderTrackingOutputBuilder.dwl"),
			payload:  samples("orderTracking-payload.json"),
			vars:     map[string]string{"itemData": samples("orderTracking-itemData.json")},
			expected: samples("orderTracking.out.json"),
		},
		{
			name:     "product filters groupBy and pluck",
			script:   samples("products-filters.dwl"),
			payload:  samples("products-filters.json"),
			expected: samples("products-filters.out.json"),
		},
		{
			name:     "query parameters with module import",
			script:   samples("query-param-payload.dwl"),
			attrs:    samples("query-param-attributes.json"),
			expected: samples("query-param-payload.out.json"),
		},
		{
			name:     "store response",
			script:   samples("stores-response.dwl"),
			payload:  samples("stores-payload.json"),
			vars:     map[string]string{"query_param": samples(storesQueryFile)},
			expected: samples("stores-response.out.json"),
		},
		{
			name:     "customer activities match and try",
			script:   samples("customer-activities.dwl"),
			payload:  samples("customer-activities-payload.json"),
			vars:     map[string]string{"query_param": samples(storesQueryFile)},
			expected: samples("customer-activities.out.json"),
		},
		{
			name:     "query builder text",
			script:   samples("stores-query-builder.dwl"),
			vars:     map[string]string{"query_param": samples("stores-query-builder.json")},
			expected: samples("stores-query-builder.out.json"),
		},
		{
			name:     "skip nulls everywhere",
			script:   samples("int-sd-es41.dwl"),
			payload:  samples("int-sd-es41.json"),
			expected: samples("int-sd-es41.out.json"),
		},
	}
}

func TestGolden(t *testing.T) {
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) { runGolden(t, tc) })
	}
}

func runGolden(t *testing.T, tc goldenCase) {
	t.Helper()
	script, err := CompileFile(tc.script, Options{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var out bytes.Buffer
	if err := script.Run(context.Background(), &out, goldenInputs(t, tc)...); err != nil {
		t.Fatalf("run: %v", err)
	}
	if tc.assert != "" {
		checkAssertion(t, tc.assert, out.Bytes())
	}
	if tc.expected != "" {
		compareGolden(t, tc.expected, out.Bytes())
	}
}

func goldenInputs(t *testing.T, tc goldenCase) []Input {
	t.Helper()
	var inputs []Input
	if tc.payload != "" {
		inputs = append(inputs, Input{Name: "payload", Path: tc.payload})
	}
	if tc.attrs != "" {
		inputs = append(inputs, Input{Name: "attributes", Path: tc.attrs})
	}
	if vars := buildVars(t, tc); vars != nil {
		inputs = append(inputs, Input{Name: "vars", Data: vars, MimeType: "application/json"})
	}
	return inputs
}

// checkAssertion runs an MUnit assertion script with output as payload.
func checkAssertion(t *testing.T, assert string, output []byte) {
	t.Helper()
	check, err := CompileFile(assert, Options{})
	if err != nil {
		t.Fatalf("compile assertion: %v", err)
	}
	if err := check.Run(context.Background(), &bytes.Buffer{}, Input{Name: "payload", Data: output}); err != nil {
		t.Fatalf("assertion failed: %v\noutput:\n%s", err, output)
	}
}

// compareGolden compares output with a golden file, rewriting it first when
// -update is set.
func compareGolden(t *testing.T, expected string, output []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(expected, output, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("read expected output: %v", err)
	}
	got := strings.ReplaceAll(string(output), "\r\n", "\n")
	if w := strings.ReplaceAll(string(want), "\r\n", "\n"); got != w {
		t.Errorf("output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", expected, got, w)
	}
}

// buildVars assembles the vars input from fixture files and expressions.
func buildVars(t *testing.T, tc goldenCase) []byte {
	t.Helper()
	if len(tc.vars) == 0 && len(tc.varExprs) == 0 {
		return nil
	}
	var fields []string
	for name, file := range tc.vars {
		fields = append(fields, `"`+name+`": `+evalFixture(t, file))
	}
	for name, expr := range tc.varExprs {
		fields = append(fields, `"`+name+`": `+evalJSON(t, expr, tc.payload))
	}
	return []byte("{" + strings.Join(fields, ",") + "}")
}

// evalFixture returns a JSON or DataWeave fixture file rendered as JSON.
func evalFixture(t *testing.T, file string) string {
	t.Helper()
	if strings.HasSuffix(file, ".json") {
		return evalJSON(t, "payload", file)
	}
	s, err := CompileFile(file, Options{})
	if err != nil {
		t.Fatalf("compile fixture %s: %v", file, err)
	}
	var buf bytes.Buffer
	if err := s.Run(context.Background(), &buf); err != nil {
		t.Fatalf("fixture %s: %v", file, err)
	}
	return buf.String()
}

// evalJSON evaluates expr with the file as payload and returns JSON.
func evalJSON(t *testing.T, expr, payload string) string {
	t.Helper()
	s, err := Compile(expr, Options{})
	if err != nil {
		t.Fatalf("compile %q: %v", expr, err)
	}
	var buf bytes.Buffer
	if err := s.Run(context.Background(), &buf, Input{Name: "payload", Path: payload}); err != nil {
		t.Fatalf("evaluate %q: %v", expr, err)
	}
	return buf.String()
}
