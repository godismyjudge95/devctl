package doctor

import (
	"context"
	"errors"
	"testing"
)

func TestRun_KeepsRegistryOrderAndMarksFixable(t *testing.T) {
	noop := func(context.Context) error { return nil }
	checks := []Check{
		{ID: "a", Title: "A", Run: func(context.Context) Finding { return Okf("fine") }},
		{ID: "b", Title: "B", Run: func(context.Context) Finding {
			f := Failf("do x", "broken")
			f.Fix = noop
			return f
		}},
		{ID: "c", Title: "C", Run: func(context.Context) Finding {
			f := Okf("fine")
			f.Fix = noop
			return f
		}},
		{ID: "d", Title: "D", Run: func(context.Context) Finding { return Warnf("", "meh") }},
	}
	report := Results(Run(context.Background(), checks))

	var ids []string
	for _, r := range report.Checks {
		ids = append(ids, r.ID)
	}
	if got := ids; len(got) != 4 || got[0] != "a" || got[1] != "b" || got[2] != "c" || got[3] != "d" {
		t.Fatalf("order = %v, want [a b c d]", got)
	}
	b := report.Checks[1]
	if b.Severity != Fail || b.Remedy != "do x" || !b.Fixable || b.Title != "B" {
		t.Errorf("b = %+v", b)
	}
	if report.Checks[2].Fixable {
		t.Error("an ok check must not be fixable even when it offers a fix")
	}
	if report.Checks[3].Fixable {
		t.Error("a check without a fix must not be fixable")
	}
	if report.Failed() != 1 {
		t.Errorf("Failed() = %d, want 1", report.Failed())
	}
}

func TestRun_PanickingCheckFailsInsteadOfCrashing(t *testing.T) {
	checks := []Check{{ID: "boom", Title: "Boom", Run: func(context.Context) Finding { panic("oops") }}}
	r := Results(Run(context.Background(), checks)).Checks[0]
	if r.ID != "boom" || r.Severity != Fail {
		t.Fatalf("result = %+v, want boom/fail", r)
	}
}

func TestApplyFixes_RunsOnlyFixableAndSelected(t *testing.T) {
	var ran []string
	fixer := func(id string, err error) func(context.Context) error {
		return func(context.Context) error {
			ran = append(ran, id)
			return err
		}
	}
	checks := []Check{
		{ID: "one", Run: func(context.Context) Finding { f := Failf("", "x"); f.Fix = fixer("one", nil); return f }},
		{ID: "two", Run: func(context.Context) Finding { f := Warnf("", "x"); f.Fix = fixer("two", errors.New("nope")); return f }},
		{ID: "three", Run: func(context.Context) Finding { f := Okf("x"); f.Fix = fixer("three", nil); return f }},
	}
	outcomes := Run(context.Background(), checks)

	all := ApplyFixes(context.Background(), outcomes, nil)
	if len(all) != 2 || all[0].ID != "one" || all[0].Error != "" || all[1].ID != "two" || all[1].Error != "nope" {
		t.Fatalf("fixes = %+v", all)
	}
	if len(ran) != 2 {
		t.Fatalf("ran = %v, want [one two]", ran)
	}

	ran = nil
	only := ApplyFixes(context.Background(), outcomes, map[string]bool{"two": true})
	if len(only) != 1 || only[0].ID != "two" || len(ran) != 1 {
		t.Fatalf("filtered fixes = %+v, ran = %v", only, ran)
	}
}
