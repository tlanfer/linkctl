package platform

import "io"

// Diagnose runs verbose camera diagnostics for this OS and writes a report
// suitable for pasting into a bug report.
func Diagnose(w io.Writer) error { return diagnose(w) }
