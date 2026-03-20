package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/ppalucha/bigfoot/scanner"
)

var (
	sizeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true) // orange
	pctStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))            // amber
	dirStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)  // blue
	fileStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))            // light gray
	barStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))            // dim
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)  // green
)

const barWidth = 20

func PrintTree(root *scanner.Entry, depth, maxDepth, topN int) {
	fmt.Printf("%s  %s\n",
		sizeStyle.Render(padSize(humanize.Bytes(uint64(root.Size)))),
		headerStyle.Render(root.Path),
	)
	printChildren(root, root.Size, depth, maxDepth, topN, "")
}

func printChildren(entry *scanner.Entry, totalSize int64, depth, maxDepth, topN int, prefix string) {
	if depth >= maxDepth {
		return
	}

	children := entry.Children
	if len(children) > topN {
		children = children[:topN]
	}

	for i, child := range children {
		isLast := i == len(children)-1
		connector := "├── "
		nextPrefix := prefix + "│   "
		if isLast {
			connector = "└── "
			nextPrefix = prefix + "    "
		}

		bar := sizeBar(child.Size, totalSize)
		pct := percent(child.Size, totalSize)
		size := sizeStyle.Render(padSize(humanize.Bytes(uint64(child.Size))))

		var name string
		if child.IsDir {
			name = dirStyle.Render(child.Path[len(entry.Path)+1:]) + "/"
		} else {
			name = fileStyle.Render(child.Path[len(entry.Path)+1:])
		}

		fmt.Printf("%s%s%s %s %s  %s\n",
			prefix, connector, size,
			barStyle.Render(bar),
			pctStyle.Render(pct),
			name,
		)

		if child.IsDir && len(child.Children) > 0 {
			printChildren(child, totalSize, depth+1, maxDepth, topN, nextPrefix)
		}
	}
}

func sizeBar(size, total int64) string {
	if total == 0 {
		return strings.Repeat("░", barWidth)
	}
	filled := int(float64(size) / float64(total) * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}

func percent(size, total int64) string {
	if total == 0 {
		return "  0.0%"
	}
	p := float64(size) / float64(total) * 100
	return fmt.Sprintf("%5.1f%%", p)
}

func padSize(s string) string {
	const width = 9
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
