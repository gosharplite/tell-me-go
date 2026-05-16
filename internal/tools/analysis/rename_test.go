package analysis

import "testing"

func TestRenamePlanDescription(t *testing.T) {
	t.Parallel()
	plan := &renamePlan{OldName: "OldFunc", NewName: "NewFunc"}
	desc := plan.Description()
	expected := "Rename OldFunc → NewFunc"
	if desc != expected {
		t.Errorf("Description() = %q, want %q", desc, expected)
	}
}
