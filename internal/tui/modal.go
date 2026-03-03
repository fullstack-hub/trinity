package tui

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/fullstack-hub/trinity/internal/session"
)

type modalMode int

const (
	modalBrowse modalMode = iota
	modalRename
)

type displayItem struct {
	isHeader bool
	header   string
	session  *session.SessionInfo
	idx      int
}

type sessionModal struct {
	visible     bool
	sessions    []session.SessionInfo
	filtered    []session.SessionInfo
	items       []displayItem
	cursor      int
	scroll      int
	search      string
	width       int // chat panel width (NOT full screen)
	height      int // chat panel height
	currentID   string
	mode        modalMode
	renameInput string

	// Screen coordinates for mouse hit testing (set by OverlayOnPanel)
	escX, escY int // top-left of "esc" text on screen
	escW       int // visual width of "esc" (3)
}

func (m *sessionModal) Open(sessions []session.SessionInfo, currentID string, w, h int) {
	m.visible = true
	m.sessions = sessions
	m.currentID = currentID
	m.cursor = 0
	m.scroll = 0
	m.search = ""
	m.width = w
	m.height = h
	m.mode = modalBrowse
	m.renameInput = ""
	m.applyFilter()
	for i, s := range m.filtered {
		if s.ID == currentID {
			m.cursor = i
			break
		}
	}
}

func (m *sessionModal) Close() {
	m.visible = false
	m.mode = modalBrowse
}

func (m *sessionModal) applyFilter() {
	if m.search == "" {
		m.filtered = m.sessions
	} else {
		query := strings.ToLower(m.search)
		m.filtered = nil
		for _, s := range m.sessions {
			if strings.Contains(strings.ToLower(s.Title), query) ||
				strings.Contains(s.ID, query) {
				m.filtered = append(m.filtered, s)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.rebuildItems()
}

func (m *sessionModal) rebuildItems() {
	m.items = nil
	lastDate := ""
	for i := range m.filtered {
		s := &m.filtered[i]
		dateStr := formatSessionDate(s.UpdatedAt)
		if dateStr != lastDate {
			m.items = append(m.items, displayItem{isHeader: true, header: dateStr})
			lastDate = dateStr
		}
		m.items = append(m.items, displayItem{session: s, idx: i})
	}
}

func (m *sessionModal) Selected() *session.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	s := m.filtered[m.cursor]
	return &s
}

func (m *sessionModal) modalWidth() int {
	w := m.width * 4 / 5
	if w > 70 {
		w = 70
	}
	if w < 40 {
		w = 40
	}
	if w > m.width-2 {
		w = m.width - 2
	}
	return w
}

// maxListRows returns how many session rows fit in the modal.
func (m *sessionModal) maxListRows() int {
	// Modal box overhead: border(2) + padding(2) + title+blank(2) + search+blank(2) + blank+hints(2) = 10
	h := m.height - 10
	if h > 15 {
		h = 15
	}
	if h < 3 {
		h = 3
	}
	return h
}

func (m *sessionModal) cursorItemIndex() int {
	for di, item := range m.items {
		if !item.isHeader && item.idx == m.cursor {
			return di
		}
	}
	return 0
}

func (m *sessionModal) ensureVisible() {
	maxVis := m.maxListRows()
	ci := m.cursorItemIndex()
	if ci > 0 && m.items[ci-1].isHeader {
		ci--
	}
	if ci < m.scroll {
		m.scroll = ci
	}
	cursorDI := m.cursorItemIndex()
	if cursorDI >= m.scroll+maxVis {
		m.scroll = cursorDI - maxVis + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *sessionModal) renderBox() string {
	mw := m.modalWidth()
	innerW := mw - 6
	if innerW < 20 {
		innerW = 20
	}

	titleSt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	searchSt := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dateSt := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	selSt := lipgloss.NewStyle().Background(lipgloss.Color("209")).Foreground(lipgloss.Color("16"))
	normSt := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dotSt := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	timeSt := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	hintK := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	hintT := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))

	var lines []string

	// Title
	var title string
	if m.mode == modalRename {
		title = titleSt.Render("Rename Session")
	} else {
		title = titleSt.Render("Sessions")
	}
	esc := dim.Render("esc")
	g := innerW - lipgloss.Width(title) - lipgloss.Width(esc)
	if g < 1 {
		g = 1
	}
	lines = append(lines, title+strings.Repeat(" ", g)+esc)
	lines = append(lines, "")

	// Search / rename
	if m.mode == modalRename {
		if m.renameInput == "" {
			lines = append(lines, dim.Render("New title")+"█")
		} else {
			lines = append(lines, searchSt.Render(m.renameInput)+"█")
		}
	} else if m.search == "" {
		lines = append(lines, dim.Render("Search"))
	} else {
		lines = append(lines, searchSt.Render(m.search)+"█")
	}
	lines = append(lines, "")

	// List
	maxVis := m.maxListRows()
	m.ensureVisible()

	vis := m.items
	if m.scroll < len(vis) {
		vis = vis[m.scroll:]
	}
	if len(vis) > maxVis {
		vis = vis[:maxVis]
	}

	for _, item := range vis {
		if item.isHeader {
			lines = append(lines, dateSt.Render(item.header))
			continue
		}
		s := item.session

		dot := "  "
		if s.ID == m.currentID {
			dot = dotSt.Render("● ")
		}

		t := s.Title
		if t == "" {
			t = "(untitled)"
		}
		tr := []rune(t)
		maxT := innerW - 14
		if maxT < 10 {
			maxT = 10
		}
		if len(tr) > maxT {
			t = string(tr[:maxT-1]) + "…"
		}

		ts := s.UpdatedAt.Format("3:04 PM")
		tp := dot + t
		eg := innerW - lipgloss.Width(tp) - len(ts)
		if eg < 1 {
			eg = 1
		}

		if item.idx == m.cursor {
			fl := tp + strings.Repeat(" ", eg) + ts
			lw := lipgloss.Width(fl)
			if lw < innerW {
				fl += strings.Repeat(" ", innerW-lw)
			}
			lines = append(lines, selSt.Render(fl))
		} else {
			lines = append(lines, normSt.Render(tp)+strings.Repeat(" ", eg)+timeSt.Render(ts))
		}
	}

	if len(m.filtered) == 0 {
		lines = append(lines, dim.Render("  No sessions found"))
	}

	lines = append(lines, "")

	if m.mode == modalRename {
		lines = append(lines, hintK.Render("enter")+" "+hintT.Render("confirm")+"  "+hintK.Render("esc")+" "+hintT.Render("cancel"))
	} else {
		lines = append(lines, hintK.Render("delete")+" "+hintT.Render("ctrl+d")+"  "+hintK.Render("rename")+" "+hintT.Render("ctrl+r"))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("242")).
		Padding(1, 2).
		Width(mw).
		Render(strings.Join(lines, "\n"))
}

// OverlayOnPanel renders the modal centered within the chat panel (not full screen).
// This ensures the sidebar is never touched.
func (m *sessionModal) OverlayOnPanel(panel string, panelW, panelH int) string {
	box := m.renderBox()
	boxLines := strings.Split(box, "\n")

	panelLines := strings.Split(panel, "\n")
	for len(panelLines) < panelH {
		panelLines = append(panelLines, strings.Repeat(" ", panelW))
	}

	startY := (panelH - len(boxLines)) / 2
	if startY < 0 {
		startY = 0
	}

	// Compute esc button screen coordinates for mouse hit testing
	// Box layout: border(1) + padding(2) = 3 cols offset, title line at row +2
	mw := m.modalWidth()
	innerW := mw - 6
	if innerW < 20 {
		innerW = 20
	}
	boxVisW := lipgloss.Width(boxLines[0])
	pad := (panelW - boxVisW) / 2
	if pad < 0 {
		pad = 0
	}
	m.escW = 3
	m.escX = pad + 3 + innerW - m.escW // "esc" is right-aligned in content
	m.escY = startY + 2                // border(1) + padding(1)

	for i, bl := range boxLines {
		y := startY + i
		if y >= 0 && y < panelH {
			w := lipgloss.Width(bl)
			pad := (panelW - w) / 2
			if pad < 0 {
				pad = 0
			}
			panelLines[y] = strings.Repeat(" ", pad) + bl
		}
	}

	if len(panelLines) > panelH {
		panelLines = panelLines[:panelH]
	}
	return strings.Join(panelLines, "\n")
}

// HitEsc returns true if the given screen coordinates are on the "esc" button.
func (m *sessionModal) HitEsc(x, y int) bool {
	return y == m.escY && x >= m.escX && x < m.escX+m.escW
}

func formatSessionDate(t time.Time) string {
	now := time.Now()
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return "Today"
	}
	yesterday := now.AddDate(0, 0, -1)
	y3, m3, d3 := yesterday.Date()
	if y2 == y3 && m2 == m3 && d2 == d3 {
		return "Yesterday"
	}
	return t.Format("Mon Jan 02 2006")
}
