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
