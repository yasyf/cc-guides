package guide

import (
	"fmt"
	"strings"
)

// UnifiedDiff returns a minimal LCS-based line diff between the on-disk artifact
// and the freshly-rendered content, labeled for human eyes on stderr.
func UnifiedDiff(label string, disk, rendered []byte) string {
	a := strings.Split(string(disk), "\n")
	b := strings.Split(string(rendered), "\n")
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s (on disk)\n+++ %s (rendered)\n", label, label)
	for _, op := range diffLines(a, b) {
		switch op.kind {
		case ' ':
			sb.WriteString("  " + op.text + "\n")
		case '-':
			sb.WriteString("- " + op.text + "\n")
		case '+':
			sb.WriteString("+ " + op.text + "\n")
		}
	}
	return sb.String()
}

type diffOp struct {
	kind byte // ' ', '-', '+'
	text string
}

func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
