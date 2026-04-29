package spec

import "testing"

func TestResult_String(t *testing.T) {
	cases := []struct {
		in   Result
		want string
	}{
		{ResultUnknown, "unknown"},
		{ResultOK, "ok"},
		{ResultPartial, "partial"},
		{ResultDone, "done"},
		{ResultBlocked, "blocked"},
		{Result(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Result(%d).String() = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResultVocab_Map(t *testing.T) {
	en := ResultVocab{OK: "ok", Partial: "partial", Done: "done", Blocked: "blocked"}
	pt := ResultVocab{OK: "ok", Partial: "parcial", Done: "finalizado", Blocked: "bloqueado"}

	cases := []struct {
		name  string
		vocab ResultVocab
		in    string
		want  Result
	}{
		{"en/ok", en, "ok", ResultOK},
		{"en/partial", en, "partial", ResultPartial},
		{"en/done", en, "done", ResultDone},
		{"en/blocked", en, "blocked", ResultBlocked},
		{"en/unknown", en, "weird", ResultUnknown},
		{"en/empty", en, "", ResultUnknown},
		{"en/trims-whitespace", en, "  ok  ", ResultOK},
		{"en/case-sensitive", en, "OK", ResultUnknown},
		{"pt/parcial", pt, "parcial", ResultPartial},
		{"pt/finalizado", pt, "finalizado", ResultDone},
		{"pt/bloqueado", pt, "bloqueado", ResultBlocked},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.vocab.Map(c.in); got != c.want {
				t.Errorf("Map(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
