package agentcontract

import "testing"

func TestTokenUsage_Total(t *testing.T) {
	cases := []struct {
		name string
		u    TokenUsage
		want int64
	}{
		{name: "zero", u: TokenUsage{}, want: 0},
		{name: "input only", u: TokenUsage{InputFresh: 100}, want: 100},
		{name: "all five buckets", u: TokenUsage{
			InputFresh: 100, InputCached: 200, CacheWrite: 50, Output: 30, Reasoning: 10,
		}, want: 390},
		{name: "provider tag does not affect total", u: TokenUsage{
			InputFresh: 1, Output: 2, Provider: ProviderAnthropic,
		}, want: 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.u.Total(); got != c.want {
				t.Errorf("Total() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestTokenUsage_IsZero(t *testing.T) {
	if !(TokenUsage{}).IsZero() {
		t.Errorf("zero value should report IsZero()")
	}
	if (TokenUsage{Provider: ProviderAnthropic}).IsZero() == false {
		// Provider tag alone does not count as usage; only token buckets do.
		t.Errorf("provider-only TokenUsage should report IsZero()")
	}
	for _, u := range []TokenUsage{
		{InputFresh: 1},
		{InputCached: 1},
		{CacheWrite: 1},
		{Output: 1},
		{Reasoning: 1},
	} {
		if u.IsZero() {
			t.Errorf("non-zero usage reports IsZero(): %+v", u)
		}
	}
}

func TestTokenUsage_Add(t *testing.T) {
	a := TokenUsage{InputFresh: 1, InputCached: 2, CacheWrite: 3, Output: 4, Reasoning: 5, Provider: ProviderAnthropic}
	b := TokenUsage{InputFresh: 10, InputCached: 20, CacheWrite: 30, Output: 40, Reasoning: 50, Provider: ProviderOpenAI}
	got := a.Add(b)
	want := TokenUsage{InputFresh: 11, InputCached: 22, CacheWrite: 33, Output: 44, Reasoning: 55, Provider: ProviderAnthropic}
	if got != want {
		t.Errorf("Add():\n got=%+v\nwant=%+v", got, want)
	}
}
