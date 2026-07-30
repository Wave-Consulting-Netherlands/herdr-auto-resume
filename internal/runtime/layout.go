package runtime

// Direction represents a spatial direction for pane navigation.
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

// Center returns the center point of the pane.
func (p *Pane) Center() (x, y int) {
	return p.Left + p.Width/2, p.Top + p.Height/2
}

// PaneByID finds a pane by ID.
func PaneByID(panes []Pane, id string) *Pane {
	for i := range panes {
		if panes[i].ID == id {
			return &panes[i]
		}
	}
	return nil
}

// PaneInDirection finds the nearest pane in the given direction from current.
func PaneInDirection(panes []Pane, current *Pane, dir Direction) *Pane {
	if current == nil || len(panes) == 0 {
		return nil
	}

	cx, cy := current.Center()
	var best *Pane
	bestDist := -1

	for i := range panes {
		p := &panes[i]
		if p.ID == current.ID {
			continue
		}

		px, py := p.Center()
		dx, dy := px-cx, py-cy

		// Check if pane is in the correct direction.
		inDirection := false
		switch dir {
		case DirLeft:
			inDirection = dx < 0 && abs(dx) > abs(dy)
		case DirRight:
			inDirection = dx > 0 && abs(dx) > abs(dy)
		case DirUp:
			inDirection = dy < 0 && abs(dy) > abs(dx)
		case DirDown:
			inDirection = dy > 0 && abs(dy) > abs(dx)
		}

		if !inDirection {
			continue
		}

		// Calculate distance (Manhattan distance).
		dist := abs(dx) + abs(dy)
		if best == nil || dist < bestDist {
			best = p
			bestDist = dist
		}
	}

	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
