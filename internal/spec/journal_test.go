package spec

import (
	"errors"
	"testing"
)

func TestParseLatestResult_English(t *testing.T) {
	vocab := ResultVocab{OK: "ok", Partial: "partial", Done: "done", Blocked: "blocked"}
	res, err := ParseLatestResult(
		loadFixture(t, "journal-en.md"),
		"## Execution Journal",
		"Result",
		vocab,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != ResultOK {
		t.Errorf("Result = %v, want ResultOK", res.Result)
	}
	if res.Raw != "ok" {
		t.Errorf("Raw = %q, want %q", res.Raw, "ok")
	}
	if res.Line == 0 {
		t.Errorf("Line should be > 0")
	}
}

func TestParseLatestResult_TakesFirstAfterHeading(t *testing.T) {
	// The fixture has TWO entries; the first (top of section) has Result: ok,
	// the second has Result: partial. Latest is the first encountered.
	vocab := ResultVocab{OK: "ok", Partial: "partial", Done: "done", Blocked: "blocked"}
	res, err := ParseLatestResult(
		loadFixture(t, "journal-en.md"),
		"## Execution Journal",
		"Result",
		vocab,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != ResultOK {
		t.Errorf("expected ResultOK (top entry), got %v", res.Result)
	}
}

func TestParseLatestResult_Portuguese(t *testing.T) {
	vocab := ResultVocab{OK: "ok", Partial: "parcial", Done: "finalizado", Blocked: "bloqueado"}
	res, err := ParseLatestResult(
		loadFixture(t, "journal-pt-br.md"),
		"## Diário de execução",
		"Resultado",
		vocab,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != ResultDone {
		t.Errorf("Result = %v, want ResultDone", res.Result)
	}
	if res.Raw != "finalizado" {
		t.Errorf("Raw = %q, want finalizado", res.Raw)
	}
}

func TestParseLatestResult_Empty(t *testing.T) {
	vocab := ResultVocab{OK: "ok"}
	_, err := ParseLatestResult(
		loadFixture(t, "journal-empty.md"),
		"## Execution Journal",
		"Result",
		vocab,
	)
	if !errors.Is(err, ErrNoResultEntry) {
		t.Errorf("err = %v, want ErrNoResultEntry", err)
	}
}

func TestParseLatestResult_HeadingNotFound(t *testing.T) {
	vocab := ResultVocab{}
	_, err := ParseLatestResult(
		"# Foo\n\n## Other\n",
		"## Execution Journal",
		"Result",
		vocab,
	)
	if !errors.Is(err, ErrJournalHeadingNotFound) {
		t.Errorf("err = %v, want ErrJournalHeadingNotFound", err)
	}
}

func TestParseLatestResult_UnknownValue(t *testing.T) {
	content := "## Execution Journal\n\n- **Result**: weird\n"
	vocab := ResultVocab{OK: "ok"}
	res, err := ParseLatestResult(content, "## Execution Journal", "Result", vocab)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != ResultUnknown {
		t.Errorf("Result = %v, want ResultUnknown", res.Result)
	}
	if res.Raw != "weird" {
		t.Errorf("Raw = %q, want %q", res.Raw, "weird")
	}
}

func TestParseLatestResult_Malformed(t *testing.T) {
	vocab := ResultVocab{OK: "ok"}
	_, err := ParseLatestResult(
		loadFixture(t, "journal-malformed.md"),
		"## Execution Journal",
		"Result",
		vocab,
	)
	if !errors.Is(err, ErrNoResultEntry) {
		t.Errorf("err = %v, want ErrNoResultEntry", err)
	}
}

func TestParseLatestResult_KeywordMismatch(t *testing.T) {
	// English journal but caller passes pt-BR keyword: should NOT match,
	// returns ErrNoResultEntry.
	vocab := ResultVocab{OK: "ok"}
	_, err := ParseLatestResult(
		loadFixture(t, "journal-en.md"),
		"## Execution Journal",
		"Resultado",
		vocab,
	)
	if !errors.Is(err, ErrNoResultEntry) {
		t.Errorf("err = %v, want ErrNoResultEntry", err)
	}
}
