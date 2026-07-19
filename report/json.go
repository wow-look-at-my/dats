package report

import (
	"encoding/json"
	"io"
	"time"

	"github.com/wow-look-at-my/dats/runner"
)

// FormatVersion is the JSON report's format_version value. Field names and
// document structure are stable within a version; a breaking change bumps it.
const FormatVersion = 1

// jsonReport is the JSON report document. Summary counts cover test
// instances only -- exactly the CLI summary numbers; file-level hook
// failures live in setup_failure/teardown_failures, never in tests.
type jsonReport struct {
	FormatVersion int         `json:"format_version"`
	Ok            bool        `json:"ok"`
	Summary       jsonSummary `json:"summary"`
	Files         []jsonFile  `json:"files"`
}

type jsonSummary struct {
	Files       int     `json:"files"`
	Tests       int     `json:"tests"`
	Passed      int     `json:"passed"`
	Failed      int     `json:"failed"`
	WallSeconds float64 `json:"wall_seconds"`
}

type jsonFile struct {
	Path             string               `json:"path"`
	Ok               bool                 `json:"ok"`
	DurationSeconds  float64              `json:"duration_seconds"`
	SetupFailure     *jsonCommandFailure  `json:"setup_failure"`
	TeardownFailures []jsonCommandFailure `json:"teardown_failures"`
	Tests            []jsonTest           `json:"tests"`
}

type jsonCommandFailure struct {
	Command string `json:"command"`
	Detail  string `json:"detail"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
}

// jsonTest is one test instance. Stdout/Stderr are pointers so the keys
// appear exactly when the instance failed (even with empty captured output)
// and never when it passed, keeping reports lean.
type jsonTest struct {
	Name            string   `json:"name"`
	Index           int      `json:"index"`
	Ok              bool     `json:"ok"`
	DurationSeconds float64  `json:"duration_seconds"`
	Failures        []string `json:"failures"`
	Command         string   `json:"command"`
	Stdout          *string  `json:"stdout,omitempty"`
	Stderr          *string  `json:"stderr,omitempty"`
}

// WriteJSON writes an indented JSON report of results to w, followed by a
// trailing newline. wall is the wall time of the whole execution phase.
// Ordering is canonical: files as given, instances in expansion order. ok
// mirrors the run's exit status (true exactly when every file passed as a
// whole -- tests, setup, and teardown); summary.tests/passed/failed equal
// the CLI summary counts, which count test instances only.
func WriteJSON(w io.Writer, results []*runner.FileResult, wall time.Duration) error {
	rep := jsonReport{
		FormatVersion: FormatVersion,
		Ok:            true,
		Summary: jsonSummary{
			Files:       len(results),
			WallSeconds: wall.Seconds(),
		},
		Files: make([]jsonFile, 0, len(results)),
	}

	for _, fr := range results {
		file := jsonFile{
			Path: fr.Path,
			Ok:   fr.Ok(),
			// Empty slices, not null, so consumers can range without care.
			TeardownFailures: make([]jsonCommandFailure, 0, len(fr.TeardownFailures)),
			Tests:            make([]jsonTest, 0, len(fr.Results)),
		}
		if !file.Ok {
			rep.Ok = false
		}
		if fr.SetupFailure != nil {
			failure := commandFailure(fr.SetupFailure)
			file.SetupFailure = &failure
		}
		for i := range fr.TeardownFailures {
			file.TeardownFailures = append(file.TeardownFailures, commandFailure(&fr.TeardownFailures[i]))
		}

		var fileDuration time.Duration
		for i := range fr.Results {
			tr := &fr.Results[i]
			fileDuration += tr.Duration
			test := jsonTest{
				Name: tr.Name,
				// The canonical 1-based instance number, matching the "ok N -"
				// numbering the CLI prints (TestResult.Index is 0-based).
				Index:           tr.Index + 1,
				Ok:              tr.Passed,
				DurationSeconds: tr.Duration.Seconds(),
				Failures:        append([]string{}, tr.Failures...),
				Command:         tr.Command,
			}
			if !tr.Passed {
				test.Stdout = &tr.Stdout
				test.Stderr = &tr.Stderr
			}
			file.Tests = append(file.Tests, test)
		}
		file.DurationSeconds = fileDuration.Seconds()

		rep.Summary.Tests += len(fr.Results)
		rep.Summary.Passed += fr.Passed
		rep.Summary.Failed += fr.Failed

		rep.Files = append(rep.Files, file)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// This is a report file, not HTML: keep >, <, & literal.
	enc.SetEscapeHTML(false)
	return enc.Encode(rep)
}

func commandFailure(fail *runner.CommandFailure) jsonCommandFailure {
	return jsonCommandFailure{
		Command: fail.Command,
		Detail:  fail.Detail,
		Stdout:  fail.Stdout,
		Stderr:  fail.Stderr,
	}
}
