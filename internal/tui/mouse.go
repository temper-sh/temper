package tui

// HitMap maps visible terminal cells to a tool's own control areas and indices.
// It belongs to one rendered frame; tools discard it when input changes state.
type HitMap struct {
	targets []controlTarget
}

type controlTarget struct {
	area int
	Target
}

func (h *HitMap) Add(area int, targets []Target) {
	for _, target := range targets {
		h.targets = append(h.targets, controlTarget{area, target})
	}
}

func (h HitMap) At(x, y int) (area, index int, ok bool) {
	for _, target := range h.targets {
		if x >= target.X && x < target.X+target.Width && y >= target.Y && y < target.Y+target.Height {
			return target.area, target.Index, true
		}
	}
	return 0, 0, false
}

func (f Frame) HeaderTargets(targets []Target) []Target {
	return f.placeTargets(targets, f.HeaderTop(), f.HeaderTop(), f.BodyTop())
}

func (f Frame) BodyTargets(targets []Target, scroll int) []Target {
	return f.placeTargets(targets, f.BodyTop()-scroll, f.BodyTop(), f.BodyTop()+f.BodyHeight())
}

func (f Frame) FooterTargets(targets []Target) []Target {
	return f.placeTargets(targets, f.FooterTop(), f.BodyTop()+f.BodyHeight(), f.Height-f.inset())
}

// Match Frame.Render's centering, scrolling and clipping. Invisible parts of
// wrapped controls must not capture clicks in another region or the margins.
func (f Frame) placeTargets(targets []Target, origin, top, bottom int) []Target {
	width := ContentWidth(f.Width)
	left := (f.Width - width) / 2
	var placed []Target
	for _, target := range targets {
		x, y := max(0, target.X), max(top, origin+target.Y)
		right, end := min(width, target.X+target.Width), min(bottom, origin+target.Y+target.Height)
		if right <= x || end <= y {
			continue
		}
		target.X, target.Y = left+x, y
		target.Width, target.Height = right-x, end-y
		placed = append(placed, target)
	}
	return placed
}
