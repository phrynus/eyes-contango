package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var tableColumnWidths = []int{1, 3, 3, 2, 2, 2, 2, 2}

type spreadUI struct {
	minPercent float64
	refresh    time.Duration

	app    *tview.Application
	header *tview.TextView
	table  *tview.Table
	footer *tview.TextView

	rows []SpreadRow
}

func runSpreadTable(ctx context.Context, limit int, refresh time.Duration, minPercent float64) {
	ui := newSpreadUI(refresh, minPercent)
	ui.run(ctx)
}

func newSpreadUI(refresh time.Duration, minPercent float64) *spreadUI {
	app := tview.NewApplication()

	header := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	table := tview.NewTable().
		SetSelectable(true, false).
		SetFixed(1, 0).
		SetBorders(true)
	table.SetBorder(true).
		SetTitle(" 实时价差 (按百分比排序) ").
		SetBorderColor(tcell.ColorDarkOrange).
		SetTitleColor(tcell.ColorLightYellow)

	footer := tview.NewTextView().
		SetDynamicColors(true)

	return &spreadUI{
		minPercent: minPercent,
		refresh:    refresh,
		app:        app,
		header:     header,
		table:      table,
		footer:     footer,
	}
}

func (ui *spreadUI) run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(ui.header, 3, 0, false).
		AddItem(ui.table, 0, 1, true)

	ui.app.SetRoot(layout, true).EnableMouse(true)

	ui.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyCtrlC {
			cancel()
			return nil
		}
		return event
	})

	go func() {
		<-ctx.Done()
		ui.app.QueueUpdateDraw(func() {
			ui.footer.SetText("[red]正在关闭 UI ...")
		})
		ui.app.Stop()
	}()

	ui.renderSnapshot()
	ui.startAutoRefresh(ctx)

	if err := ui.app.Run(); err != nil && ctx.Err() == nil {
		log.Printf("UI 运行错误: %v", err)
	}
}

func (ui *spreadUI) startAutoRefresh(ctx context.Context) {
	refresh := ui.refresh
	if refresh <= 0 {
		refresh = 250 * time.Millisecond
	}

	ticker := time.NewTicker(refresh)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ui.refreshOnceAsync()
			}
		}
	}()
}

func (ui *spreadUI) refreshOnceAsync() {
	_, height := 0, 0
	ui.app.QueueUpdate(func() {
		_, _, _, h := ui.table.GetInnerRect()
		height = h
	})
	limit := ui.calculateDisplayLimit(height)
	rows := globalTickers.snapshotTopSpreads(limit, ui.minPercent)
	total := globalTickers.totalSymbols()

	ui.app.QueueUpdateDraw(func() {
		ui.applySnapshot(rows, total)
	})
}

func (ui *spreadUI) renderSnapshot() {
	_, _, _, height := ui.table.GetInnerRect()
	limit := ui.calculateDisplayLimit(height)
	rows := globalTickers.snapshotTopSpreads(limit, ui.minPercent)
	total := globalTickers.totalSymbols()
	ui.applySnapshot(rows, total)
}

// calculateDisplayLimit 根据窗口高度计算可显示的行数
func (ui *spreadUI) calculateDisplayLimit(height int) int {
	// 高度 = header(3) + table_border(2) + footer(1) + header_row(1)
	// 可用高度 = height - 7
	availableHeight := height - 8
	if availableHeight < 1 {
		availableHeight = 10 // 最小显示10行
	}
	limit := availableHeight
	if appConfig.TableLimit > 0 && limit > appConfig.TableLimit {
		limit = appConfig.TableLimit
	}
	return limit
}

func (ui *spreadUI) applySnapshot(rows []SpreadRow, total int) {
	ui.rows = rows
	ui.renderHeader(total, len(rows))
	ui.renderTable(rows)
}

func (ui *spreadUI) renderHeader(tracked, displayed int) {
	connected := globalExchanges.GetConnectedCount()
	total := globalExchanges.GetTotalCount()
	header := fmt.Sprintf(
		"[::b]Contango 实时价差监控[-:-:-]  [gray]|[-]  交易所: [%s]%d/%d[-]  [gray]|[-]  过滤: [white]≥ %.2f%%[-]  [gray]|[-]  已追踪: [white]%d[-]  [gray]|[-]  当前显示: [white]%d[-]",
		getConnectionColor(connected, total),
		connected,
		total,
		ui.minPercent,
		tracked,
		displayed,
	)
	ui.header.SetText(header)
}

// getConnectionColor 根据连接状态返回颜色
func getConnectionColor(connected, total int) string {
	if connected == total && total > 0 {
		return "green" // 全部连接
	} else if connected > 0 {
		return "yellow" // 部分连接
	}
	return "red" // 无连接
}

func (ui *spreadUI) renderTable(rows []SpreadRow) {
	ui.table.Clear()

	headers := []string{"#", "Symbol", "Pair", "Spread %", "Spread", "High (Bid)", "Low (Bid)", "Age"}
	for col, title := range headers {
		ui.table.SetCell(0, col, fixedWidthCell(col, fmt.Sprintf("[white::b]%s", title)).
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
	}

	if len(rows) == 0 {
		ui.table.SetCell(1, 0, tview.NewTableCell("[gray]等待行情 ...").
			SetAlign(tview.AlignCenter).
			SetSelectable(false))
		return
	}

	for idx, row := range rows {
		color := colorForSpread(row.Spread.SpreadPercent)
		age := humanizeDuration(time.Since(time.UnixMilli(row.UpdatedAt)))

		ui.table.SetCell(idx+1, 0, fixedWidthCell(0, fmt.Sprintf("%d", idx+1)).SetAlign(tview.AlignRight))
		ui.table.SetCell(idx+1, 1, fixedWidthCell(1, row.Symbol).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 2, fixedWidthCell(2, row.Spread.ExchangePair).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 3, fixedWidthCell(3, fmt.Sprintf("%s%.2f%%[-]", color, row.Spread.SpreadPercent)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 4, fixedWidthCell(4, fmt.Sprintf("%s%.4f[-]", color, row.Spread.Spread)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 5, fixedWidthCell(5, fmt.Sprintf("%.4f", row.Spread.HighBid)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 6, fixedWidthCell(6, fmt.Sprintf("%.4f", row.Spread.LowBid)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 7, fixedWidthCell(7, age).SetAlign(tview.AlignCenter))
	}
}

func fixedWidthCell(col int, text string) *tview.TableCell {
	expansion := tableColumnWidths[col]
	return tview.NewTableCell(text).
		SetExpansion(expansion)
}

func (ui *spreadUI) onSelectionChanged(row, _ int) {
	if row <= 0 || row-1 >= len(ui.rows) {
		ui.footer.SetText("[gray]鼠标滚轮可滚动")
		return
	}

	sel := ui.rows[row-1]
	ui.footer.SetText(fmt.Sprintf(
		"[white]%s[-]  [gray]|[-]  价差: [yellow]%.2f%%[-] (%s)  [gray]|[-]  高: %s [green]%.4f[-]  [gray]|[-]  低: %s [red]%.4f[-]",
		sel.Symbol,
		sel.Spread.SpreadPercent,
		sel.Spread.ExchangePair,
		sel.Spread.HighExchange,
		sel.Spread.HighBid,
		sel.Spread.LowExchange,
		sel.Spread.LowBid,
	))
}

func colorForSpread(percent float64) string {
	switch {
	case percent >= 3:
		return "[#00FF9C]"
	case percent >= 1.5:
		return "[#3BFF8F]"
	case percent >= 0.5:
		return "[#F1C40F]"
	case percent >= 0:
		return "[#F39C12]"
	default:
		return "[#FF6B6B]"
	}
}

func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}

	minutes := int(d / time.Minute)
	seconds := int(d/time.Second) % 60
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}
