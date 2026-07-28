package contract

import (
	"errors"
	"testing"

	"github.com/miaoxiaoyong/sift/internal/decode"
)

// V14 golden suite (DESIGN §12, WBS M1 §1.1).
//
// Closed contracts assert that extra fields and required-field absence are both
// rejected. Open-envelope contracts assert that unrelated extra fields are
// accepted (forward compatibility) while required/type/enum rules on consumed
// fields still hold. Type and enum variants are covered for both.

// noErr marks a golden case that must decode successfully. It must not
// collide with any real decode.Kind (the iota sequence starts at 0).
const noErr decode.Kind = -1

func TestV14ClosedExample(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr decode.Kind // noErr = accept
		check   func(t *testing.T, v ClosedExample)
	}{
		{
			name:    "valid_minimal",
			json:    `{"name":"fix-bug","severity":"low"}`,
			wantErr: noErr,
			check: func(t *testing.T, v ClosedExample) {
				if v.Name == nil || *v.Name != "fix-bug" {
					t.Fatalf("name = %v", v.Name)
				}
				if v.Severity == nil || *v.Severity != SeverityLow {
					t.Fatalf("severity = %v", v.Severity)
				}
				if v.Timeout != nil {
					t.Fatalf("timeout should be nil, got %v", *v.Timeout)
				}
			},
		},
		{
			name:    "valid_with_optional",
			json:    `{"name":"fix-bug","severity":"high","timeout":"5s"}`,
			wantErr: noErr,
			check: func(t *testing.T, v ClosedExample) {
				if v.Timeout == nil || *v.Timeout != "5s" {
					t.Fatalf("timeout = %v", v.Timeout)
				}
			},
		},
		{
			name:    "missing_required_name",
			json:    `{"severity":"low"}`,
			wantErr: decode.KindMissingRequired,
		},
		{
			name:    "missing_required_severity",
			json:    `{"name":"x"}`,
			wantErr: decode.KindMissingRequired,
		},
		{
			// Required + JSON null is the same as absent.
			name:    "required_field_null",
			json:    `{"name":"x","severity":null}`,
			wantErr: decode.KindMissingRequired,
		},
		{
			name:    "extra_field_rejected",
			json:    `{"name":"x","severity":"low","unknown":7}`,
			wantErr: decode.KindUnknownField,
		},
		{
			name:    "wrong_type_name_is_number",
			json:    `{"name":123,"severity":"low"}`,
			wantErr: decode.KindInvalidType,
		},
		{
			name:    "wrong_type_severity_is_number",
			json:    `{"name":"x","severity":5}`,
			wantErr: decode.KindInvalidType,
		},
		{
			name:    "wrong_enum_severity",
			json:    `{"name":"x","severity":"urgent"}`,
			wantErr: decode.KindInvalidEnum,
		},
		{
			name:    "empty_enum_value",
			json:    `{"name":"x","severity":""}`,
			wantErr: decode.KindInvalidEnum,
		},
		{
			name:    "trailing_data",
			json:    `{"name":"x","severity":"low"}{}`,
			wantErr: decode.KindTrailingData,
		},
		{
			name:    "malformed_json",
			json:    `{"name":"x","severity":`,
			wantErr: decode.KindInvalidJSON,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v ClosedExample
			err := decode.Decode([]byte(tc.json), &v, decode.Closed)
			if tc.wantErr == noErr {
				if err != nil {
					t.Fatalf("expected accept, got %v", err)
				}
				if tc.check != nil {
					tc.check(t, v)
				}
				return
			}
			if k := kindOf(t, err); k != tc.wantErr {
				t.Fatalf("expected %s, got %s (%v)", tc.wantErr, k, err)
			}
		})
	}
}

func TestV14OpenEnvelopeExample(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr decode.Kind
		check   func(t *testing.T, v OpenEnvelopeExample)
	}{
		{
			name:    "valid_minimal",
			json:    `{"number":42,"title":"issue","state":"open"}`,
			wantErr: noErr,
			check: func(t *testing.T, v OpenEnvelopeExample) {
				if v.Number == nil || *v.Number != 42 {
					t.Fatalf("number = %v", v.Number)
				}
				if v.State == nil || *v.State != ForgeIssueOpen {
					t.Fatalf("state = %v", v.State)
				}
			},
		},
		{
			// Core V14 open-envelope assertion: unrelated extra fields are
			// accepted for forward compatibility.
			name:    "extra_unknown_field_accepted",
			json:    `{"number":42,"title":"issue","state":"open","node_id":"ABC","labels":["bug"]}`,
			wantErr: noErr,
			check: func(t *testing.T, v OpenEnvelopeExample) {
				if v.Number == nil || *v.Number != 42 {
					t.Fatalf("number = %v", v.Number)
				}
			},
		},
		{
			name:    "missing_required_number",
			json:    `{"title":"issue","state":"open"}`,
			wantErr: decode.KindMissingRequired,
		},
		{
			name:    "missing_required_state",
			json:    `{"number":1,"title":"issue"}`,
			wantErr: decode.KindMissingRequired,
		},
		{
			name:    "consumed_wrong_type_number_is_string",
			json:    `{"number":"42","title":"issue","state":"open"}`,
			wantErr: decode.KindInvalidType,
		},
		{
			name:    "consumed_wrong_enum_state",
			json:    `{"number":1,"title":"issue","state":"merged"}`,
			wantErr: decode.KindInvalidEnum,
		},
		{
			name:    "consumed_wrong_type_in_extra_field_does_not_matter",
			wantErr: noErr,
			// An extra field with a wrong type relative to nothing is still
			// ignored: open-envelope does not validate unknown fields at all.
			json: `{"number":1,"title":"issue","state":"open","random":{"nested":[1,2]}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v OpenEnvelopeExample
			err := decode.Decode([]byte(tc.json), &v, decode.OpenEnvelope)
			if tc.wantErr == noErr {
				if err != nil {
					t.Fatalf("expected accept, got %v", err)
				}
				if tc.check != nil {
					tc.check(t, v)
				}
				return
			}
			if k := kindOf(t, err); k != tc.wantErr {
				t.Fatalf("expected %s, got %s (%v)", tc.wantErr, k, err)
			}
		})
	}
}

// TestV14ClosedVsOpenContrast drives the identical payload through both modes
// to make the policy difference explicit and regression-proof: the extra field
// is the only difference, and only closed mode rejects it.
func TestV14ClosedVsOpenContrast(t *testing.T) {
	payload := []byte(`{"number":1,"title":"t","state":"open","extra":"ignored"}`)

	var oe OpenEnvelopeExample
	if err := decode.Decode(payload, &oe, decode.OpenEnvelope); err != nil {
		t.Fatalf("open-envelope must accept extra field, got %v", err)
	}

	var ce ClosedExample
	// ClosedExample has different fields; the point is that closed mode rejects
	// the extra field. Use a closed-shaped payload to isolate the policy.
	cePayload := []byte(`{"name":"x","severity":"low","extra":1}`)
	if err := decode.Decode(cePayload, &ce, decode.Closed); err == nil {
		t.Fatal("closed mode must reject extra field")
	}
}

// TestEnumSingleSource pins the invariant that Severity's and ForgeIssueState's
// EnumValues match their constants: the runtime check and the generated schema
// both derive from EnumValues, so a constant added without updating it is a
// silent gap this test catches.
func TestEnumSingleSource(t *testing.T) {
	want := map[Severity]bool{SeverityLow: true, SeverityNormal: true, SeverityHigh: true}
	got := Severity("").EnumValues()
	if len(got) != len(want) {
		t.Fatalf("severity enum count %d != %d", len(got), len(want))
	}
	for _, v := range got {
		if !want[Severity(v)] {
			t.Fatalf("severity enum has unexpected value %q", v)
		}
	}

	wantState := map[ForgeIssueState]bool{ForgeIssueOpen: true, ForgeIssueClosed: true}
	gotState := ForgeIssueState("").EnumValues()
	if len(gotState) != len(wantState) {
		t.Fatalf("forge state enum count %d != %d", len(gotState), len(wantState))
	}
	for _, v := range gotState {
		if !wantState[ForgeIssueState(v)] {
			t.Fatalf("forge state enum has unexpected value %q", v)
		}
	}
}

func kindOf(t *testing.T, err error) decode.Kind {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var de *decode.DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("expected *decode.DecodeError, got %T: %v", err, err)
	}
	return de.Kind
}
