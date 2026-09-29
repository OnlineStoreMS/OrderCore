package service

import "testing"

func TestBasePlatformSysTid(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"abc", "abc"},
		{"abc#split12", "abc"},
		{"abc#dup99", "abc"},
		{"abc#alloc1", "abc"},
	}
	for _, c := range cases {
		if got := basePlatformSysTid(c.in); got != c.want {
			t.Fatalf("basePlatformSysTid(%q)=%q want %q", c.in, got, c.want)
		}
	}
}
