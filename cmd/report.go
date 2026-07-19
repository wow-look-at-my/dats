package cmd

// The --report-junit/--report-json flags: machine-readable report files.
// Registration and the write-to-disk plumbing live here; the document
// rendering itself is the report package. Reports are written by runTests
// whenever the run executed -- especially when tests failed and the process
// is about to exit 1 -- from the same results in both serial and jobs mode.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/pflag"
	"github.com/wow-look-at-my/dats/report"
	"github.com/wow-look-at-my/dats/runner"
)

var (
	reportJUnit string
	reportJSON  string
)

// registerReportFlags registers --report-junit and --report-json on flags.
// Both are long-only (no shorthand) and require a value; both may be used at
// once. Empty (absent) means no report of that kind is written.
func registerReportFlags(flags *pflag.FlagSet) {
	flags.StringVar(&reportJUnit, "report-junit", "",
		"write a JUnit XML report of the run to the given file")
	flags.StringVar(&reportJSON, "report-json", "",
		"write a JSON report of the run to the given file")
}

// writeReports writes every requested report file from the run's results.
// Both reports are attempted even if the first fails; a failure to write a
// report is a real error (stderr message, exit 1) even when every test
// passed.
func writeReports(results []*runner.FileResult, wall time.Duration) error {
	var errs []error
	if reportJUnit != "" {
		errs = append(errs, writeReportFile(reportJUnit, func(w io.Writer) error {
			return report.WriteJUnit(w, results, wall)
		}))
	}
	if reportJSON != "" {
		errs = append(errs, writeReportFile(reportJSON, func(w io.Writer) error {
			return report.WriteJSON(w, results, wall)
		}))
	}
	return errors.Join(errs...)
}

// writeReportFile creates path -- parent directories included -- and renders
// one report into it via write.
func writeReportFile(path string, write func(io.Writer) error) error {
	fail := func(err error) error {
		return fmt.Errorf("writing report %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fail(err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fail(err)
	}
	if err := write(f); err != nil {
		f.Close()
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fail(err)
	}
	return nil
}
