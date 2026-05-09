package dag

import (
	"testing"
)

func TestTools_ReturnsToolDescriptors(t *testing.T) {
	tools, err := Tools()
	if err != nil {
		t.Fatalf("Tools() unexpected error: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("Tools() returned empty slice; expected at least one descriptor")
	}
	for _, td := range tools {
		if td.Name == "" {
			t.Errorf("ToolDescriptor with empty Name: %+v", td)
		}
		if td.InputSchema == nil {
			t.Errorf("ToolDescriptor %q has nil InputSchema", td.Name)
		}
	}
}

func TestTools_SortedByName(t *testing.T) {
	tools, err := Tools()
	if err != nil {
		t.Fatalf("Tools() unexpected error: %v", err)
	}
	for i := 1; i < len(tools); i++ {
		if tools[i].Name < tools[i-1].Name {
			t.Errorf("Tools() not sorted: %q comes before %q", tools[i-1].Name, tools[i].Name)
		}
	}
}

func TestTools_DescriptorType(t *testing.T) {
	tools, err := Tools()
	if err != nil {
		t.Fatalf("Tools() unexpected error: %v", err)
	}
	// Compile-time assertion: Tools() must return []ToolDescriptor.
	// This table confirms each element is addressable as a ToolDescriptor.
	for _, td := range tools {
		var _ ToolDescriptor = td
	}
}
