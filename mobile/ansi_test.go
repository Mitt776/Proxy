package mobile

import "testing"

func TestStripANSI(t *testing.T) {
	const esc = "\x1b"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"без цвета", "INFO router: match[0] => sniff", "INFO router: match[0] => sniff"},
		{
			// Ровно то, что приезжает от ядра на Android.
			"строка ядра",
			esc + "[36mINFO" + esc + "[0m[0052] inbound/mixed: connection to ipwho.is:443",
			"INFO[0052] inbound/mixed: connection to ipwho.is:443",
		},
		{"пусто", "", ""},
		{"только код", esc + "[0m", ""},
		{"незакрытая последовательность", "a" + esc + "[36", "a"},
		{"esc без скобки", "a" + esc + "b", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripANSI(c.in); got != c.want {
				t.Fatalf("stripANSI(%q) = %q, ожидалось %q", c.in, got, c.want)
			}
		})
	}
}
