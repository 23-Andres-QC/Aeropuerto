package services

import "testing"

func TestMaskEmail(t *testing.T) {
	for in, want := range map[string]string{"andres@gmail.com": "an****@gmail.com", "a@x.com": "a****@x.com", "sinarroba": "sinarroba", "@x.com": "@x.com"} {
		if got := maskEmail(in); got != want {
			t.Errorf("maskEmail(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}
