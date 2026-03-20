package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
	"github.com/ppalucha/bigfoot/scanner"
)

var (
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	sepStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("237"))
	selectedDir = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true).Underline(true)
	selectedFil = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
)

type treeNode struct {
	entry    *scanner.Entry
	depth    int
	expanded bool
}

type TUIModel struct {
	root      *scanner.Entry
	totalSize int64
	nodes     []*treeNode
	cursor    int
	offset    int
	height    int
	width     int
}

func NewTUIModel(root *scanner.Entry) TUIModel {
	m := TUIModel{
		root:      root,
		totalSize: root.Size,
		height:    24,
		width:     80,
	}
	for _, child := range root.Children {
		m.nodes = append(m.nodes, &treeNode{entry: child, depth: 0})
	}
	return m
}

func (m TUIModel) Init() tea.Cmd { return nil }

func (m TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.ensureVisible()
			}
		case "down", "j":
			if m.cursor < len(m.nodes)-1 {
				m.cursor++
				m.ensureVisible()
			}
		case "right", "l":
			m.expand()
		case "left", "h":
			m.collapse()
		case "enter":
			if m.cursor < len(m.nodes) && m.nodes[m.cursor].expanded {
				m.collapse()
			} else {
				m.expand()
			}
		case "g":
			m.cursor = 0
			m.offset = 0
		case "G":
			m.cursor = len(m.nodes) - 1
			m.ensureVisible()
		case "pgdown", " ":
			m.cursor = min(m.cursor+m.viewHeight(), len(m.nodes)-1)
			m.ensureVisible()
		case "pgup":
			m.cursor = max(m.cursor-m.viewHeight(), 0)
			m.ensureVisible()
		}
	}
	return m, nil
}

func (m *TUIModel) expand() {
	if m.cursor >= len(m.nodes) {
		return
	}
	node := m.nodes[m.cursor]
	if !node.entry.IsDir || node.expanded || len(node.entry.Children) == 0 {
		return
	}
	node.expanded = true
	children := make([]*treeNode, len(node.entry.Children))
	for i, child := range node.entry.Children {
		children[i] = &treeNode{entry: child, depth: node.depth + 1}
	}
	tail := append([]*treeNode{}, m.nodes[m.cursor+1:]...)
	m.nodes = append(m.nodes[:m.cursor+1], append(children, tail...)...)
}

func (m *TUIModel) collapse() {
	if m.cursor >= len(m.nodes) {
		return
	}
	node := m.nodes[m.cursor]
	if node.expanded {
		m.collapseAt(m.cursor)
	} else if node.depth > 0 {
		// jump to parent and collapse it
		for i := m.cursor - 1; i >= 0; i-- {
			if m.nodes[i].depth < node.depth {
				m.cursor = i
				m.collapseAt(i)
				m.ensureVisible()
				return
			}
		}
	}
}

func (m *TUIModel) collapseAt(idx int) {
	node := m.nodes[idx]
	node.expanded = false
	depth := node.depth
	end := idx + 1
	for end < len(m.nodes) && m.nodes[end].depth > depth {
		end++
	}
	m.nodes = append(m.nodes[:idx+1], m.nodes[end:]...)
}

func (m *TUIModel) ensureVisible() {
	vh := m.viewHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	} else if m.cursor >= m.offset+vh {
		m.offset = m.cursor - vh + 1
	}
}

func (m TUIModel) viewHeight() int {
	h := m.height - 4 // 2 header + 2 footer lines
	if h < 1 {
		h = 1
	}
	return h
}

func (m TUIModel) View() string {
	var b strings.Builder

	// Header line 1: root size + path
	// Header line 2: selected item's full path (context while navigating)
	selectedPath := ""
	if m.cursor < len(m.nodes) {
		selectedPath = m.nodes[m.cursor].entry.Path
	}
	b.WriteString(fmt.Sprintf("  %s  %s\n",
		sizeStyle.Render(padSize(humanize.Bytes(uint64(m.root.Size)))),
		headerStyle.Render(m.root.Path),
	))
	b.WriteString(dimStyle.Render("  "+selectedPath) + "\n")

	b.WriteString(sepStyle.Render(strings.Repeat("─", m.width)) + "\n")

	// Tree rows
	vh := m.viewHeight()
	end := m.offset + vh
	if end > len(m.nodes) {
		end = len(m.nodes)
	}
	rendered := 0
	for i := m.offset; i < end; i++ {
		b.WriteString(m.renderRow(m.nodes[i], i == m.cursor) + "\n")
		rendered++
	}
	for rendered < vh {
		b.WriteString("\n")
		rendered++
	}

	// Footer
	b.WriteString(sepStyle.Render(strings.Repeat("─", m.width)) + "\n")
	position := fmt.Sprintf("%d/%d", m.cursor+1, len(m.nodes))
	help := "↑↓/jk move  →←/Enter expand/collapse  g/G top/end  q quit"
	b.WriteString(dimStyle.Render(help + "  " + position))

	return b.String()
}

func (m TUIModel) renderRow(node *treeNode, selected bool) string {
	indent := strings.Repeat("  ", node.depth)

	cur := "  "
	if selected {
		cur = cursorStyle.Render("▶ ")
	}

	var toggle string
	if node.entry.IsDir {
		if node.expanded {
			toggle = "▼ "
		} else if len(node.entry.Children) > 0 {
			toggle = "▶ "
		} else {
			toggle = "  "
		}
	} else {
		toggle = "  "
	}

	bar := sizeBar(node.entry.Size, m.totalSize)
	pct := percent(node.entry.Size, m.totalSize)
	size := sizeStyle.Render(padSize(humanize.Bytes(uint64(node.entry.Size))))

	name := node.entry.Path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}

	var nameStr string
	if node.entry.IsDir {
		if selected {
			nameStr = selectedDir.Render(name) + "/"
		} else {
			nameStr = dirStyle.Render(name) + "/"
		}
	} else {
		if selected {
			nameStr = selectedFil.Render(name)
		} else {
			nameStr = fileStyle.Render(name)
		}
	}

	return fmt.Sprintf("%s%s%s%s %s %s  %s",
		cur, indent, toggle, size,
		barStyle.Render(bar),
		pctStyle.Render(pct),
		nameStr,
	)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
