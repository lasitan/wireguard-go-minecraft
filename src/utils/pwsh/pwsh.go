// Package pwsh helps build PowerShell command strings.
package pwsh

import "strings"

// Quote escapes s for use inside a single-quoted PowerShell string.
func Quote(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
