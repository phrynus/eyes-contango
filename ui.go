package main

import (
	"context"
	"fmt"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var tableColumnWidths = []int{1, 3, 3, 2, 2, 2}

type spreadUI struct {
	minPercent float64
	refresh    time.Duration

	app    *tview.Application
	header *tview.TextView
	table  *tview.Table
	footer *tview.TextView

	rows []SpreadRow

	// 固定行：记录当前“锁定”的行（根据 Symbol + ExchangePair 唯一标识）
	pinnedKey string
	pinnedRow int
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

	ui := &spreadUI{
		minPercent: minPercent,
		refresh:    refresh,
		app:        app,
		header:     header,
		table:      table,
		footer:     footer,
	}

	// 注册选中行变化回调，用于更新底部信息和固定当前选中行
	table.SetSelectionChangedFunc(ui.onSelectionChanged)

	return ui
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
		log.Errorf("UI 运行错误: %v", err)
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

// calculateDisplayLimit 计算一次快照要拉取的最大行数
// 规则：
// - 如果配置了 tableLimit (>0)，优先使用配置值，列表可滚动，高度只影响可见区域，不影响总行数
// - 如果 tableLimit <=0，则退回到根据窗口高度估算一个合理的行数
func (ui *spreadUI) calculateDisplayLimit(height int) int {
	// 显式配置了上限时，直接使用配置的数量，让用户完全控制“拉多少条数据”
	if appConfig.TableLimit > 0 {
		return appConfig.TableLimit
	}

	// 未配置上限（<=0）时，按照窗口高度估算一个显示行数
	// 高度 = header(3) + table_border(2) + footer(1) + header_row(1)
	// 可用高度 = height - 7
	availableHeight := height - 8
	if availableHeight < 1 {
		availableHeight = 10 // 最小显示10行
	}
	return availableHeight
}

func (ui *spreadUI) applySnapshot(rows []SpreadRow, total int) {
	// 先根据当前固定配置调整行顺序
	ordered := ui.applyPinning(rows)
	ui.rows = ordered
	ui.renderHeader(total, len(rows))
	ui.renderTable(ordered)
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

	// 表头说明：
	// - Pair: BUY@A->SELL@B 方向性套利
	// - Spread % / Spread: 基于买入 Ask 与卖出 Bid 的价差
	// - Sell (Bid): 卖出价格（Bid）
	// - Buy (Ask): 买入价格（Ask）
	headers := []string{" # ", " 币种 ", " 多空 ", " 价差 % ", "卖", "买", " 延迟 "}
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
		ui.table.SetCell(idx+1, 1, fixedWidthCell(1, formatPairDisplay(row.Symbol)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 2, fixedWidthCell(2, row.Spread.ExchangePair).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 3, fixedWidthCell(3, fmt.Sprintf("%s%.2f%%[-]", color, row.Spread.SpreadPercent)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 5, fixedWidthCell(4, fmt.Sprintf("%.4f", row.Spread.LowBid)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 4, fixedWidthCell(5, fmt.Sprintf("%.4f", row.Spread.HighBid)).SetAlign(tview.AlignCenter))
		ui.table.SetCell(idx+1, 6, fixedWidthCell(6, age).SetAlign(tview.AlignCenter))
	}
}

func fixedWidthCell(col int, text string) *tview.TableCell {
	var expansion int
	if col >= 0 && col < len(tableColumnWidths) {
		expansion = tableColumnWidths[col]
	} else {
		expansion = 1 // 默认扩展比例
	}
	return tview.NewTableCell(text).
		SetExpansion(expansion)
}

// rowKey 返回用于标识一条行记录的唯一 key（Symbol + 方向）
func rowKey(r SpreadRow) string {
	return r.Symbol + "|" + r.Spread.ExchangePair
}

// applyPinning 根据 pinnedKey / pinnedRow 将选中的币固定在指定位置
// 注意：这里使用的是「表格行号」（1 开始），内部切片是 0 开始，所以需要减 1。
func (ui *spreadUI) applyPinning(rows []SpreadRow) []SpreadRow {
	if ui.pinnedKey == "" || ui.pinnedRow <= 0 || len(rows) == 0 {
		return rows
	}

	targetIdx := ui.pinnedRow - 1 // 转成 0-based
	if targetIdx < 0 {
		targetIdx = 0
	}
	if targetIdx >= len(rows) {
		targetIdx = len(rows) - 1
	}

	// 找出需要固定的那一条记录
	var pinned *SpreadRow
	others := make([]SpreadRow, 0, len(rows))
	for _, r := range rows {
		if pinned == nil && rowKey(r) == ui.pinnedKey {
			tmp := r
			pinned = &tmp
			continue
		}
		others = append(others, r)
	}

	// 如果当前快照里已经没有这条记录，就不做固定
	if pinned == nil {
		return rows
	}

	// 重新组装有序切片：在 targetIdx 位置放 pinned，其它位置按原排序依次填充
	ordered := make([]SpreadRow, 0, len(rows))
	otherIdx := 0
	for i := 0; i < len(rows); i++ {
		if i == targetIdx {
			ordered = append(ordered, *pinned)
		} else {
			ordered = append(ordered, others[otherIdx])
			otherIdx++
		}
	}

	return ordered
}

func (ui *spreadUI) onSelectionChanged(row, _ int) {
	if row <= 0 || row-1 >= len(ui.rows) {
		ui.footer.SetText("[gray]鼠标滚轮可滚动")
		return
	}

	sel := ui.rows[row-1]

	// 记录当前选中行为“固定行”：后续数据刷新 / 重新排序时，该行会保持在当前表格位置
	ui.pinnedKey = rowKey(sel)
	ui.pinnedRow = row

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

// formatPairDisplay 将交易对格式化为简洁形式
// 例如：BTC/USDT:USDT -> BTC/USDT
func formatPairDisplay(pair string) string {
	// 如果包含 ":", 则只显示 ":" 之前的部分
	if idx := len(pair) - 1; idx >= 0 {
		for i := len(pair) - 1; i >= 0; i-- {
			if pair[i] == ':' {
				return pair[:i]
			}
		}
	}
	return pair
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
