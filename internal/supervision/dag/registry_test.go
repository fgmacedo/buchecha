package dag

import (
	"strings"
	"testing"
	"time"
)

func TestRegister_AssignsIDWithRolePrefix(t *testing.T) {
	r := NewAgentRegistry(func() time.Time { return time.Unix(0, 0) })
	id, err := r.Register(RoleExecutor, RegisterArgs{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !strings.HasPrefix(string(id), string(RoleExecutor)+"-") {
		t.Errorf("agent_id %q lacks role prefix", string(id))
	}
}

func TestRegister_RejectsInvalidRole(t *testing.T) {
	r := NewAgentRegistry(nil)
	if _, err := r.Register(Role("not-a-role"), RegisterArgs{}); err == nil {
		t.Error("expected error for invalid role")
	}
}

func TestLookup_ReturnsRegisteredEntry(t *testing.T) {
	r := NewAgentRegistry(nil)
	id, err := r.Register(RoleReviewer, RegisterArgs{
		BriefingID: "iter-1",
		PhaseID:    "P1",
		SubDAG:     []TaskID{"t1", "t2"},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Lookup(id)
	if !ok {
		t.Fatal("Lookup not found")
	}
	if got.Role != RoleReviewer {
		t.Errorf("role = %q, want %q", string(got.Role), string(RoleReviewer))
	}
	if got.BriefingID != "iter-1" || got.PhaseID != "P1" {
		t.Errorf("scope mismatch: %+v", got)
	}
	if len(got.SubDAG) != 2 || got.SubDAG[0] != "t1" || got.SubDAG[1] != "t2" {
		t.Errorf("SubDAG = %v, want [t1 t2]", got.SubDAG)
	}
}

func TestLookup_RejectsUnknown(t *testing.T) {
	r := NewAgentRegistry(nil)
	if _, ok := r.Lookup(AgentID("nope")); ok {
		t.Error("Lookup should be false for unknown id")
	}
}

func TestDeregister_IsIdempotent(t *testing.T) {
	r := NewAgentRegistry(nil)
	id, err := r.Register(RolePlanner, RegisterArgs{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	r.Deregister(id)
	if _, ok := r.Lookup(id); ok {
		t.Error("Lookup should be false after Deregister")
	}
	r.Deregister(id) // should not panic or error
}

func TestRegister_LookupCopyIndependence(t *testing.T) {
	r := NewAgentRegistry(nil)
	id, err := r.Register(RoleExecutor, RegisterArgs{
		SubDAG: []TaskID{"t1", "t2"},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, _ := r.Lookup(id)
	got.SubDAG[0] = "mutated"
	again, _ := r.Lookup(id)
	if again.SubDAG[0] != "t1" {
		t.Error("Lookup must return a deep copy of SubDAG so callers cannot mutate registry state")
	}
}
