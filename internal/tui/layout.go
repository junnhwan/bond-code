package tui

func CalculateLayout(width, height, reservedBottomHeight int) LayoutState {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if reservedBottomHeight < 1 {
		reservedBottomHeight = 1
	}

	headerH := 0
	footerH := 1
	maxReservedBottomH := height - headerH - footerH - 1
	if maxReservedBottomH < 1 {
		maxReservedBottomH = 1
	}
	if reservedBottomHeight > maxReservedBottomH {
		reservedBottomHeight = maxReservedBottomH
	}
	timelineH := height - headerH - footerH - reservedBottomHeight
	if timelineH < 1 {
		timelineH = 1
	}

	return LayoutState{
		Width:     width,
		Height:    height,
		HeaderH:   headerH,
		TimelineW: max(1, width),
		TimelineH: timelineH,
		ComposerH: reservedBottomHeight,
		FooterH:   footerH,
	}
}
