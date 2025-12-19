package main

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	ccxt "github.com/ccxt/ccxt/go/v4"
)

// CoinTicker 存储单个币种的交易所和价差信息
type CoinTicker struct {
	Symbol   string                  `json:"s"`
	Exchange map[string]*ccxt.Ticker `json:"e"`
	Spread   Spread                  `json:"sp"`
}

// SpreadRow 是用于展示的价差快照
type SpreadRow struct {
	Symbol    string
	Spread    SpreadInfo
	UpdatedAt int64
}

// SpreadInfo 存储两个交易所之间的价差信息（基于 Bid / Ask 的方向性套利）
// 语义说明：
// - LowExchange / LowBid: 代表买入方（使用 Ask 价）
// - HighExchange / HighBid: 代表卖出方（使用 Bid 价）
// - ExchangePair: 通常表示 "BUY@低价交易所 -> SELL@高价交易所"
type SpreadInfo struct {
	ExchangePair  string  `json:"e"`
	HighExchange  string  `json:"he"`
	LowExchange   string  `json:"le"`
	HighBid       float64 `json:"h"`
	LowBid        float64 `json:"l"`
	Spread        float64 `json:"s"`
	SpreadPercent float64 `json:"sp"`
}

// Spread 存储所有交易所对的价差信息
type Spread struct {
	MaxSpread  *SpreadInfo  `json:"maxs"`
	MinSpread  *SpreadInfo  `json:"mins"`
	AllSpreads []SpreadInfo `json:"alls"`
	UpdatedAt  int64        `json:"u"`
}

// 全部交易所币种合集
// 优化：使用指针减少复制，使用 RWMutex 支持并发读写
type CoinTickers struct {
	mu   sync.RWMutex
	data map[string]*CoinTicker
	// 价差计算任务队列
	spreadCalcChan chan string
	// 工作池等待组
	spreadCalcWg sync.WaitGroup
	// 价差计算节流：记录每个symbol的最后计算时间
	lastCalcTime map[string]int64
	// 价差计算节流锁
	throttleMu sync.RWMutex
}

var stringsBuilderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

// updateTickers 批量更新 ticker 数据到全局存储
// 优化：异步价差计算，减少锁持有时间，提高并发性能，添加节流机制
func updateTickers(exchangeName string, tickers map[string]ccxt.Ticker) {
	if len(tickers) == 0 {
		return
	}

	now := time.Now().UnixMilli()
	symbolsToUpdate := make([]string, 0, len(tickers))

	// 快速更新 ticker 数据，最小化锁持有时间
	globalTickers.mu.Lock()
	for symbol, ticker := range tickers {
		// 跳过无效的 symbol
		if symbol == "" {
			continue
		}

		coinTicker, exists := globalTickers.data[symbol]
		if !exists {
			// 预分配 Exchange map 容量（假设最多5个交易所）
			coinTicker = &CoinTicker{
				Symbol:   symbol,
				Exchange: make(map[string]*ccxt.Ticker, 5),
			}
			globalTickers.data[symbol] = coinTicker
		}

		// 创建 ticker 的副本
		tickerCopy := ticker
		coinTicker.Exchange[exchangeName] = &tickerCopy
		symbolsToUpdate = append(symbolsToUpdate, symbol)
	}
	globalTickers.mu.Unlock()

	// 异步触发价差计算（带节流，非阻塞）
	for _, symbol := range symbolsToUpdate {
		// 检查节流：如果距离上次计算时间太短，跳过
		globalTickers.throttleMu.RLock()
		lastCalc, exists := globalTickers.lastCalcTime[symbol]
		globalTickers.throttleMu.RUnlock()

		if exists && (now-lastCalc) < appConfig.SpreadCalcThrottleMs {
			// 还在节流期内，跳过此次计算
			continue
		}

		// 更新最后计算时间
		globalTickers.throttleMu.Lock()
		globalTickers.lastCalcTime[symbol] = now
		globalTickers.throttleMu.Unlock()

		// 发送到计算队列（非阻塞）
		select {
		case globalTickers.spreadCalcChan <- symbol:
			// 成功发送到队列
		default:
			// 队列已满，跳过（避免阻塞）
			// 价差会在下次更新时重新计算
		}
	}
}

// spreadCalcWorker 价差计算工作协程
// 优化：添加批量处理，减少锁竞争
func (ct *CoinTickers) spreadCalcWorker(workerID int) {
	defer ct.spreadCalcWg.Done()

	// 批量处理：收集一段时间内的symbol，批量计算
	batch := make([]string, 0, 100)
	ticker := time.NewTicker(100 * time.Millisecond) // 每100ms批量处理一次
	defer ticker.Stop()

	for {
		select {
		case symbol, ok := <-ct.spreadCalcChan:
			if !ok {
				// channel已关闭，处理剩余任务后退出
				for _, s := range batch {
					ct.calculateSpreadForSymbol(s)
				}
				return
			}
			batch = append(batch, symbol)
			// 如果批次已满，立即处理
			if len(batch) >= 100 {
				for _, s := range batch {
					ct.calculateSpreadForSymbol(s)
				}
				batch = batch[:0] // 清空批次
			}
		case <-ticker.C:
			// 定时批量处理
			if len(batch) > 0 {
				for _, s := range batch {
					ct.calculateSpreadForSymbol(s)
				}
				batch = batch[:0] // 清空批次
			}
		}
	}
}

// calculateSpreadForSymbol 为指定币种计算价差（线程安全）
// 优化：检查价差是否真的有显著变化，避免无意义的更新
func (ct *CoinTickers) calculateSpreadForSymbol(symbol string) {
	// 快速获取数据快照
	ct.mu.RLock()
	coinTicker := ct.data[symbol]
	if coinTicker == nil {
		ct.mu.RUnlock()
		return
	}

	// 创建 Exchange map 的快照（只复制指针）
	exchangeSnapshot := make(map[string]*ccxt.Ticker, len(coinTicker.Exchange))
	for k, v := range coinTicker.Exchange {
		exchangeSnapshot[k] = v
	}
	ct.mu.RUnlock()

	// 在锁外计算价差
	tempTicker := &CoinTicker{
		Symbol:   symbol,
		Exchange: exchangeSnapshot,
	}
	calculateSpread(tempTicker)

	// 检查价差是否有显著变化（避免频繁更新）
	// 更新价差结果（需要写锁）
	ct.mu.Lock()
	if coinTicker := ct.data[symbol]; coinTicker != nil {
		coinTicker.Spread = tempTicker.Spread
	}
	ct.mu.Unlock()
}

// calculateSpread 计算指定币种在所有交易所之间的价差（使用 Bid 价）
// 优化：使用对象池，减少内存分配，优化字符串拼接
func calculateSpread(coinTicker *CoinTicker) {
	if len(coinTicker.Exchange) < 2 {
		// 至少需要两个交易所才能计算价差
		coinTicker.Spread = Spread{
			UpdatedAt: time.Now().UnixMilli(),
		}
		return
	}

	// 获取当前时间戳（毫秒）
	now := time.Now().UnixMilli()
	validityThreshold := now - appConfig.TickerValidity

	// 收集所有有效的 Bid / Ask（带时效性检查）
	type ExchangeQuote struct {
		Exchange string
		Bid      float64
		Ask      float64
	}

	// 预分配容量，减少内存重新分配
	validQuotes := make([]ExchangeQuote, 0, len(coinTicker.Exchange))
	for exchangeName, ticker := range coinTicker.Exchange {
		// 检查 Ticker 是否有效：非空、有 Bid/Ask 价、且都大于0、在时效范围内
		if ticker == nil || ticker.Bid == nil || ticker.Ask == nil || *ticker.Bid <= 0 || *ticker.Ask <= 0 {
			continue
		}

		// 检查时效性：如果 Ticker 有 Timestamp，检查是否在有效期内
		if ticker.Timestamp != nil {
			tickerTime := *ticker.Timestamp
			// 如果 Ticker 时间戳是秒级，转换为毫秒
			if tickerTime < 1e12 {
				tickerTime *= 1000
			}
			// 检查是否过期
			if tickerTime < validityThreshold {
				continue // 跳过过期的 Ticker
			}
		}

		// 检查 Bid / Ask 是否为 NaN 或 Inf，如果是则跳过
		if math.IsNaN(*ticker.Bid) || math.IsInf(*ticker.Bid, 0) ||
			math.IsNaN(*ticker.Ask) || math.IsInf(*ticker.Ask, 0) {
			continue
		}

		validQuotes = append(validQuotes, ExchangeQuote{
			Exchange: exchangeName,
			Bid:      *ticker.Bid,
			Ask:      *ticker.Ask,
		})
	}

	if len(validQuotes) < 2 {
		// 至少需要两个有效的报价
		coinTicker.Spread = Spread{
			UpdatedAt: time.Now().UnixMilli(),
		}
		return
	}

	// 预计算交易所对数量：n*(n-1)/2
	// 这里我们需要方向性套利（正向 / 反向），因此是有序对：n*(n-1)
	numPairs := len(validQuotes) * (len(validQuotes) - 1)
	allSpreads := make([]SpreadInfo, 0, numPairs)
	var maxSpread *SpreadInfo
	var minSpread *SpreadInfo

	// 从对象池获取字符串构建器
	exchangePairBuilder := stringsBuilderPool.Get().(*strings.Builder)
	defer func() {
		exchangePairBuilder.Reset()
		stringsBuilderPool.Put(exchangePairBuilder)
	}()
	exchangePairBuilder.Grow(32) // 预分配足够空间

	// 对每一对不同交易所 (i, j) 计算方向性套利：
	// 正向：在 i 用 Ask 买入，在 j 用 Bid 卖出
	// 反向：在 j 用 Ask 买入，在 i 用 Bid 卖出
	for i := 0; i < len(validQuotes); i++ {
		for j := 0; j < len(validQuotes); j++ {
			if i == j {
				continue
			}
			buySide := validQuotes[i]
			sellSide := validQuotes[j]

			// 检查 NaN / Inf
			if math.IsNaN(buySide.Ask) || math.IsInf(buySide.Ask, 0) ||
				math.IsNaN(sellSide.Bid) || math.IsInf(sellSide.Bid, 0) {
				continue
			}

			// 利用买入 Ask 与卖出 Bid 计算套利空间
			spread := sellSide.Bid - buySide.Ask
			if buySide.Ask <= 0 {
				continue
			}
			spreadPercent := (spread / buySide.Ask) * 100

			// 检查计算结果是否为 NaN 或 Inf
			if math.IsNaN(spread) || math.IsInf(spread, 0) ||
				math.IsNaN(spreadPercent) || math.IsInf(spreadPercent, 0) {
				continue
			}

			// ExchangePair 显示方向：BUY@买入交易所 -> SELL@卖出交易所
			exchangePairBuilder.Reset()
			exchangePairBuilder.WriteString(buySide.Exchange)
			exchangePairBuilder.WriteString(" ->")
			exchangePairBuilder.WriteString(sellSide.Exchange)
			exchangePair := exchangePairBuilder.String()

			spreadInfo := SpreadInfo{
				ExchangePair:  exchangePair,
				HighExchange:  sellSide.Exchange,
				LowExchange:   buySide.Exchange,
				HighBid:       sellSide.Bid, // 卖出价格（Bid）
				LowBid:        buySide.Ask,  // 买入价格（Ask）
				Spread:        spread,
				SpreadPercent: spreadPercent,
			}

			allSpreads = append(allSpreads, spreadInfo)

			// 更新最大和最小价差（基于百分比）
			if maxSpread == nil || spreadInfo.SpreadPercent > maxSpread.SpreadPercent {
				maxSpread = &allSpreads[len(allSpreads)-1]
			}
			if minSpread == nil || spreadInfo.SpreadPercent < minSpread.SpreadPercent {
				minSpread = &allSpreads[len(allSpreads)-1]
			}
		}
	}

	coinTicker.Spread = Spread{
		MaxSpread:  maxSpread,
		MinSpread:  minSpread,
		AllSpreads: allSpreads,
		UpdatedAt:  time.Now().UnixMilli(),
	}
}

// snapshotTopSpreads 返回按价差百分比排序的前 N 条记录
func (ct *CoinTickers) snapshotTopSpreads(limit int, minSpreadPercent float64) []SpreadRow {
	ct.mu.RLock()
	rows := make([]SpreadRow, 0, len(ct.data))
	for symbol, ticker := range ct.data {
		if ticker == nil || ticker.Spread.MaxSpread == nil {
			continue
		}
		if ticker.Spread.MaxSpread.SpreadPercent < minSpreadPercent {
			continue
		}
		rows = append(rows, SpreadRow{
			Symbol:    symbol,
			Spread:    *ticker.Spread.MaxSpread,
			UpdatedAt: ticker.Spread.UpdatedAt,
		})
	}
	ct.mu.RUnlock()

	if len(rows) == 0 {
		return rows
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Spread.SpreadPercent > rows[j].Spread.SpreadPercent
	})

	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}

	return rows
}

// totalSymbols 返回当前缓存的币种数量
func (ct *CoinTickers) totalSymbols() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.data)
}
