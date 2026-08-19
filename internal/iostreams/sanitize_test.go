package iostreams

import "testing"

func TestSanitizeForTerminal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "Acme Corp", "Acme Corp"},
		{"unicode kept", "Ăcmé ✓ 名前", "Ăcmé ✓ 名前"},
		{"csi color", "\x1b[31mAcme\x1b[0m", "Acme"},
		{"csi cursor", "evil\x1b[2Jclear", "evilclear"},
		{"osc bel", "\x1b]0;title\x07Acme", "Acme"},
		{"osc st", "\x1b]8;;https://evil.example\x1b\\Acme", "Acme"},
		{"bare esc", "A\x1bcB", "AB"},
		{"trailing esc", "Acme\x1b", "Acme"},
		{"c0 controls", "A\x00c\tm\re\n!", "Acme!"},
		{"del", "Acme\x7f", "Acme"},
		{"raw c1 csi", "Acme\u009b31mRed", "Acme31mRed"},
	}
	for _, tc := range cases {
		if got := SanitizeForTerminal(tc.in); got != tc.want {
			t.Errorf("%s: SanitizeForTerminal(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}
