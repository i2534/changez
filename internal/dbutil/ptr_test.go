package dbutil

import "testing"

func TestAsInt64Ptr(t *testing.T) {
	v := int64(42)

	cases := []struct {
		name    string
		in      any
		wantVal int64
		wantOK  bool
	}{
		{"nil interface", nil, 0, false},
		{"typed nil *int64", (*int64)(nil), 0, false},
		{"non-nil *int64", &v, 42, true},
		{"wrong type", "abc", 0, false},
		{"wrong type *string", new(string), 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AsInt64Ptr(tc.in)
			if ok != tc.wantOK || got != tc.wantVal {
				t.Fatalf("AsInt64Ptr(%v) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}

func TestAsStringPtr(t *testing.T) {
	v := "hello"

	cases := []struct {
		name    string
		in      any
		wantVal string
		wantOK  bool
	}{
		{"nil interface", nil, "", false},
		{"typed nil *string", (*string)(nil), "", false},
		{"non-nil *string", &v, "hello", true},
		{"wrong type", 123, "", false},
		{"wrong type *int64", new(int64), "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AsStringPtr(tc.in)
			if ok != tc.wantOK || got != tc.wantVal {
				t.Fatalf("AsStringPtr(%v) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.wantVal, tc.wantOK)
			}
		})
	}
}
