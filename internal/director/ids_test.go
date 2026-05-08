package director

import "testing"

func TestSpecHash_StableAcrossCalls(t *testing.T) {
	in := []byte("# title\n\nbody.\n")
	a := SpecHash(in)
	b := SpecHash(in)
	if a != b {
		t.Fatalf("non-deterministic SpecHash: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("SpecHash length = %d, want 64 hex chars", len(a))
	}
}

func TestSpecHash_DifferentInputsDiffer(t *testing.T) {
	a := SpecHash([]byte("a"))
	b := SpecHash([]byte("b"))
	if a == b {
		t.Fatalf("hash collision on tiny inputs")
	}

	withBOM := SpecHash([]byte{0xEF, 0xBB, 0xBF, 'a'})
	if a == withBOM {
		t.Fatalf("BOM did not change hash")
	}
}

func TestPhaseID_Stable(t *testing.T) {
	hash := SpecHash([]byte("spec content"))
	a := PhaseID(hash, "implement domain types")
	b := PhaseID(hash, "implement domain types")
	if a != b {
		t.Fatalf("PhaseID not stable: %s vs %s", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("PhaseID length = %d, want 16", len(a))
	}
}

func TestPhaseID_DifferentInputsDiffer(t *testing.T) {
	h1 := SpecHash([]byte("spec one"))
	h2 := SpecHash([]byte("spec two"))

	cases := [][2]string{
		{PhaseID(h1, "intent A"), PhaseID(h1, "intent B")},
		{PhaseID(h1, "same intent"), PhaseID(h2, "same intent")},
	}
	for i, pair := range cases {
		if pair[0] == pair[1] {
			t.Errorf("case %d: collision on differing inputs", i)
		}
	}
}

// TestPhaseID_NoNullByteCollision pins down that PhaseID does not
// collapse (specHash + intent) and (specHash || intent) into the same
// digest: the null-byte separator must isolate the two segments.
func TestPhaseID_NoNullByteCollision(t *testing.T) {
	a := PhaseID("abc", "def")
	b := PhaseID("abcd", "ef")
	if a == b {
		t.Fatalf("null-byte separator missing: %s == %s", a, b)
	}
}

func TestComputeSessionHash_AllCasesYield64HexChars(t *testing.T) {
	cases := []struct {
		name    string
		spec    []byte
		prompt  string
		wantEq  string
		wantDif []string
	}{
		{
			name:   "spec only",
			spec:   []byte("abc"),
			prompt: "",
			wantEq: "spec-hash",
		},
		{
			name:   "prompt only",
			spec:   nil,
			prompt: "hi",
			wantDif: []string{
				"spec-hash-of-prompt",
				"prompt-only-different-string",
			},
		},
		{
			name:   "both differ",
			spec:   []byte("abc"),
			prompt: "hi",
			wantDif: []string{
				"spec-only",
				"prompt-only",
			},
		},
		{
			name:   "prompt change",
			spec:   []byte("abc"),
			prompt: "hi",
			wantDif: []string{
				"same-spec-different-prompt",
			},
		},
		{
			name:   "deterministic",
			spec:   []byte("abc"),
			prompt: "hi",
		},
		{
			name:   "both empty",
			spec:   nil,
			prompt: "",
			wantEq: "spec-hash",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeSessionHash(tc.spec, tc.prompt)

			// Must always be 64 lowercase hex characters
			if len(got) != 64 {
				t.Fatalf("length = %d, want 64", len(got))
			}
			for _, ch := range got {
				if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
					t.Fatalf("non-hex character %q in %q", ch, got)
				}
			}

			// Check if it should equal a specific hash type
			if tc.wantEq == "spec-hash" {
				if want := SpecHash(tc.spec); got != want {
					t.Fatalf("does not equal SpecHash: got %s, want %s", got, want)
				}
			}
		})
	}
}

func TestComputeSessionHash_SpecOnly(t *testing.T) {
	spec := []byte("abc")
	got := ComputeSessionHash(spec, "")
	want := SpecHash(spec)
	if got != want {
		t.Fatalf("spec-only: ComputeSessionHash(spec, \"\") != SpecHash(spec)")
	}
}

func TestComputeSessionHash_PromptOnly(t *testing.T) {
	prompt := "hi"
	hash1 := ComputeSessionHash(nil, prompt)
	hash2 := ComputeSessionHash(nil, prompt)

	// Must be deterministic
	if hash1 != hash2 {
		t.Fatalf("prompt-only not deterministic: %s != %s", hash1, hash2)
	}

	// Must differ from SpecHash of the prompt bytes
	specHashOfPrompt := SpecHash([]byte(prompt))
	if hash1 == specHashOfPrompt {
		t.Fatalf("prompt-only should differ from SpecHash([]byte(prompt))")
	}
}

func TestComputeSessionHash_BothDiffer(t *testing.T) {
	specOnly := ComputeSessionHash([]byte("abc"), "")
	promptOnly := ComputeSessionHash(nil, "hi")
	both := ComputeSessionHash([]byte("abc"), "hi")

	if specOnly == promptOnly {
		t.Fatalf("spec-only equals prompt-only")
	}
	if specOnly == both {
		t.Fatalf("spec-only equals both")
	}
	if promptOnly == both {
		t.Fatalf("prompt-only equals both")
	}
}

func TestComputeSessionHash_PromptChangeTriggersDiff(t *testing.T) {
	spec := []byte("abc")
	hash1 := ComputeSessionHash(spec, "hi")
	hash2 := ComputeSessionHash(spec, "bye")
	if hash1 == hash2 {
		t.Fatalf("different prompts produced same hash")
	}
}

func TestComputeSessionHash_BothEmptyEqualsSpecHash(t *testing.T) {
	got := ComputeSessionHash(nil, "")
	want := SpecHash(nil)
	if got != want {
		t.Fatalf("both empty: ComputeSessionHash(nil, \"\") != SpecHash(nil)")
	}
}
