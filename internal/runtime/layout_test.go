package runtime

import "testing"

func TestPaneByID(t *testing.T) {
	panes := []Pane{{ID: "p1"}, {ID: "p2"}}

	tests := []struct {
		name string
		id   string
		want string
	}{
		{name: "found", id: "p2", want: "p2"},
		{name: "missing", id: "p3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PaneByID(panes, tt.id)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("PaneByID(%q) = %#v, want nil", tt.id, got)
				}
				return
			}
			if got == nil || got.ID != tt.want {
				t.Fatalf("PaneByID(%q) = %#v, want pane %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestPaneInDirection(t *testing.T) {
	panes := []Pane{
		{ID: "current", Left: 40, Top: 40, Width: 10, Height: 10},
		{ID: "left-near", Left: 20, Top: 40, Width: 10, Height: 10},
		{ID: "left-far", Left: 0, Top: 40, Width: 10, Height: 10},
		{ID: "right-near", Left: 60, Top: 40, Width: 10, Height: 10},
		{ID: "right-far", Left: 90, Top: 40, Width: 10, Height: 10},
		{ID: "up-near", Left: 40, Top: 20, Width: 10, Height: 10},
		{ID: "up-far", Left: 40, Top: 0, Width: 10, Height: 10},
		{ID: "down-near", Left: 40, Top: 60, Width: 10, Height: 10},
		{ID: "down-far", Left: 40, Top: 90, Width: 10, Height: 10},
	}
	current := PaneByID(panes, "current")

	tests := []struct {
		dir  Direction
		want string
	}{
		{dir: DirLeft, want: "left-near"},
		{dir: DirRight, want: "right-near"},
		{dir: DirUp, want: "up-near"},
		{dir: DirDown, want: "down-near"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := PaneInDirection(panes, current, tt.dir)
			if got == nil || got.ID != tt.want {
				t.Fatalf("PaneInDirection(%v) = %#v, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestPaneInDirectionNoNeighbor(t *testing.T) {
	current := Pane{ID: "current", Left: 40, Top: 40, Width: 10, Height: 10}
	panes := []Pane{current}

	for _, dir := range []Direction{DirLeft, DirRight, DirUp, DirDown} {
		if got := PaneInDirection(panes, &panes[0], dir); got != nil {
			t.Errorf("PaneInDirection(%v) = %#v, want nil", dir, got)
		}
	}
	if got := PaneInDirection(panes, nil, DirLeft); got != nil {
		t.Fatalf("PaneInDirection with nil current = %#v, want nil", got)
	}
}

func TestPaneInDirectionTieKeepsFirstPane(t *testing.T) {
	panes := []Pane{
		{ID: "current", Left: 40, Top: 40, Width: 10, Height: 10},
		{ID: "first", Left: 20, Top: 40, Width: 10, Height: 10},
		{ID: "second", Left: 20, Top: 40, Width: 10, Height: 10},
	}
	got := PaneInDirection(panes, &panes[0], DirLeft)
	if got == nil || got.ID != "first" {
		t.Fatalf("PaneInDirection tie = %#v, want first pane", got)
	}
}
