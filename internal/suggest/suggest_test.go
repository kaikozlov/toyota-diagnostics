package suggest

import (
	"reflect"
	"testing"
)

func TestClosest(t *testing.T) {
	pool := []string{"frc", "frs", "eps", "search", "inspect", "steering_angle", "monitor"}
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"frcc", []string{"frc"}},
		{"serach", []string{"search"}},
		{"inspec", []string{"inspect"}},
		{"steeering", []string{"steering_angle"}},
		{"zzzzzz", nil},
		{"", nil},
	} {
		got := Closest(tc.query, pool, 3)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: got %v want %v", tc.query, got, tc.want)
		}
	}
}

func TestClosestLimitsAndDeduplicates(t *testing.T) {
	got := Closest("abs", []string{"abs_p3", "abs_p3", "abs_p5", "abs_p6", "absx"}, 2)
	if len(got) != 2 || got[0] != "absx" {
		t.Fatalf("%v", got)
	}
}

func TestTyposAreSingleEditsNotPrefixes(t *testing.T) {
	pool := []string{"search", "did", "customize", "monitor", "can", "inspect", "vehicle"}
	if got := Typos("serach", pool, 2); !reflect.DeepEqual(got, []string{"search"}) {
		t.Fatalf("serach: %v", got)
	}
	for _, q := range []string{"didactic", "customized", "monitoring", "canbus"} {
		if got := Typos(q, pool, 2); got != nil {
			t.Fatalf("%q should not look like a command typo: %v", q, got)
		}
	}
}
