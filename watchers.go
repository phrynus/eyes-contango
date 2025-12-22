package main

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ccxtpro "github.com/ccxt/ccxt/go/v4/pro"
)

// exchangeInit 在每轮筛选时由各交易所发送其黑名单筛选后的币种列表，并提供接收响应的通道
type exchangeInit struct {
	Name    string
	Symbols []string
	Resp    chan []string
}

// watchExchange 为单个交易所处理订阅逻辑
func watchExchange(exchangeName string, exchange ccxtpro.IExchange, blacklist map[string]bool, sendCh chan<- exchangeInit, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Infof("%s: 开始初始化交易所监听器", exchangeName)

	// 初始化交易所状态为未连接
	globalExchanges.UpdateStatus(exchangeName, false)

	// 用于存储当前订阅的批次 goroutine，以便在重新筛选时停止旧的订阅
	var currentBatches []func() // 存储停止函数
	var batchesMutex sync.Mutex
	// 跟踪活跃的批次数量，用于状态管理
	var activeBatches int32

	// 启动订阅的函数
	startWatching := func(symbols []string) {
		if len(symbols) == 0 {
			log.Warnf("%s: No contract symbols found", exchangeName)
			globalExchanges.UpdateStatus(exchangeName, false)
			return
		}

		// 将符号列表分成批次（预分配容量）
		batches := splitSymbolsIntoBatches(symbols, appConfig.BatchSize)
		log.Infof("%s: 准备启动 %d 个批次，共 %d 个交易对", exchangeName, len(batches), len(symbols))

		// 为每个批次创建一个 goroutine 来订阅
		var batchWg sync.WaitGroup
		batchWg.Add(len(batches))

		stopChannels := make([]chan bool, len(batches))

		for i, batch := range batches {
			stopChan := make(chan bool, 1)
			stopChannels[i] = stopChan

			go func(batchNum int, batchSymbols []string, stopCh chan bool) {
				defer batchWg.Done()
				// 增加活跃批次计数
				atomic.AddInt32(&activeBatches, 1)
				log.Infof("%s - Batch %d: 启动批次，包含 %d 个交易对", exchangeName, batchNum, len(batchSymbols))

				time.Sleep(300 * time.Millisecond)
				watchBatchWithStop(exchangeName, exchange, batchNum, batchSymbols, stopCh, &activeBatches)

				// 减少活跃批次计数
				remaining := atomic.AddInt32(&activeBatches, -1)

				// 如果所有批次都停止了，更新状态为未连接
				if remaining == 0 {
					log.Infof("%s: 所有批次已停止，更新状态为未连接", exchangeName)
					globalExchanges.UpdateStatus(exchangeName, false)
				}
			}(i+1, batch, stopChan)
		}

		// 保存停止函数
		batchesMutex.Lock()
		currentBatches = make([]func(), len(stopChannels))
		for i := range stopChannels {
			idx := i
			currentBatches[idx] = func() {
				select {
				case stopChannels[idx] <- true:
				default:
				}
			}
		}
		batchesMutex.Unlock()
	}

	// 筛选并启动订阅的函数
	filterAndStart := func() {
		log.Infof("%s: 开始获取市场数据...", exchangeName)
		// 获取所有市场（带重试机制）
		markets, err := fetchMarketsWithRetry(exchange, exchangeName)
		if err != nil {
			log.Errorf("%s: Failed to fetch markets after retries: %v", exchangeName, err)
			globalExchanges.UpdateStatus(exchangeName, false)
			return
		}
		log.Infof("%s: 成功获取 %d 个市场", exchangeName, len(markets))

		// 先筛选出符合条件的币种（格式和黑名单）
		contractSymbols := filterContractSymbols(markets, blacklist)
		log.Infof("%s: 格式和黑名单筛选后剩余 %d 个交易对", exchangeName, len(contractSymbols))

		// 根据成交量进一步筛选（在多交易所存在性过滤之前）
		contractSymbols = filterSymbolsByVolume(exchangeName, exchange, contractSymbols)
		log.Infof("%s: 成交量筛选后剩余 %d 个交易对", exchangeName, len(contractSymbols))

		// 将本交易所经过黑名单和成交量筛选后的结果发送到协调器，等待所有交易所完成多交易所存在性统计后，接收去除仅存在于单一交易所的币种列表
		if sendCh != nil {
			respCh := make(chan []string, 1)
			sendCh <- exchangeInit{
				Name:    exchangeName,
				Symbols: contractSymbols,
				Resp:    respCh,
			}
			// 等待协调器返回经过“多交易所存在性”过滤后的列表
			adjusted := <-respCh
			contractSymbols = adjusted
			log.Infof("%s: 去除仅存在于单一交易所后剩余 %d 个交易对", exchangeName, len(contractSymbols))
		}

		// 停止旧的订阅
		batchesMutex.Lock()
		oldBatchCount := len(currentBatches)
		log.Infof("%s: 停止 %d 个旧批次", exchangeName, oldBatchCount)
		for _, stop := range currentBatches {
			if stop != nil {
				stop()
			}
		}
		currentBatches = nil
		batchesMutex.Unlock()

		// 等待一小段时间，确保旧的订阅已停止
		// 同时重置activeBatches计数，因为旧批次会异步减少计数
		time.Sleep(2 * time.Second)
		// 重置计数器（旧批次应该已经停止并减少了计数）
		atomic.StoreInt32(&activeBatches, 0)

		// 启动新的订阅
		startWatching(contractSymbols)
	}

	// 立即执行一次筛选和订阅
	filterAndStart()

	// 每小时重新筛选一次
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		log.Infof("%s: 开始每小时重新筛选币种...", exchangeName)
		filterAndStart()
	}
}

// checkStopSignal 检查停止信号，如果收到则返回true
func checkStopSignal(stopCh chan bool, exchangeName string, batchNum int) bool {
	if stopCh == nil {
		return false
	}
	select {
	case <-stopCh:
		log.Infof("%s - Batch %d: 收到停止信号，退出订阅", exchangeName, batchNum)
		return true
	default:
		return false
	}
}

// watchBatchWithStop 处理单个批次的订阅，带重试机制和停止信号
// 优化：使用带停止信号的sleep，避免忙等待，降低CPU占用
func watchBatchWithStop(exchangeName string, exchange ccxtpro.IExchange, batchNum int, symbols []string, stopCh chan bool, activeBatches *int32) {

	retryCount := 0
	delay := appConfig.InitialRetryDelay
	hasConnected := false

	for {
		// 检查是否需要停止（非阻塞）
		if checkStopSignal(stopCh, exchangeName, batchNum) {
			return
		}

		bidsAsks, err := exchange.WatchBidsAsks(
			ccxtpro.WithWatchBidsAsksSymbols(symbols),
		)
		if err != nil {
			if hasConnected {
				hasConnected = false
				remaining := atomic.LoadInt32(activeBatches)
				if remaining <= 1 {
					globalExchanges.UpdateStatus(exchangeName, false)
				}
			}

			// 再次检查停止信号
			if checkStopSignal(stopCh, exchangeName, batchNum) {
				return
			}

			retryCount++
			if retryCount <= appConfig.MaxRetries {
				log.Warnf("%s - Batch %d: WatchBidsAsks失败 (retry %d/%d): %v, retrying in %ds",
					exchangeName, batchNum, retryCount, appConfig.MaxRetries, err, delay)

				// 等待重试延迟，同时检查停止信号
				if stopCh != nil {
					select {
					case <-stopCh:
						log.Infof("%s - Batch %d: 收到停止信号，退出订阅", exchangeName, batchNum)
						return
					case <-time.After(time.Duration(delay) * time.Second):
					}
				} else {
					time.Sleep(time.Duration(delay) * time.Second)
				}

				delay *= 2
				if delay > 30 {
					delay = 30
				}
				continue
			} else {
				log.Errorf("%s - Batch %d: Max retries reached, stopping", exchangeName, batchNum)
				return
			}
		}

		tickers := bidsAsks.Tickers

		// 成功连接后重置重试计数和延迟
		if !hasConnected {
			log.Infof("%s - Batch %d: 成功连接（使用WatchBidsAsks），开始接收数据 (%d 个交易对)", exchangeName, batchNum, len(symbols))
			hasConnected = true
		}
		retryCount = 0
		delay = appConfig.InitialRetryDelay

		// 更新为已连接状态（只要有至少一个批次连接成功）
		globalExchanges.UpdateStatus(exchangeName, true)

		// 处理接收到的数据并实时更新到存储
		if len(tickers) > 0 {

			updateTickers(exchangeName, tickers)
		} else {
			log.Debugf("%s - Batch %d: 收到空 ticker 数据", exchangeName, batchNum)
		}
	}
}

// fetchMarketsWithRetry 带重试机制获取市场数据
func fetchMarketsWithRetry(exchange ccxtpro.IExchange, exchangeName string) ([]ccxtpro.MarketInterface, error) {
	var markets []ccxtpro.MarketInterface
	var err error
	delay := appConfig.InitialRetryDelay

	for i := 0; i < appConfig.MaxRetries; i++ {
		log.Debugf("%s: 尝试获取市场数据 (第 %d/%d 次)", exchangeName, i+1, appConfig.MaxRetries)
		markets, err = exchange.FetchMarkets()
		if err == nil {
			log.Infof("%s: 成功获取市场数据，共 %d 个市场", exchangeName, len(markets))
			return markets, nil
		}

		if i < appConfig.MaxRetries-1 {
			log.Warnf("%s: Error fetching markets (attempt %d/%d): %v, retrying in %ds",
				exchangeName, i+1, appConfig.MaxRetries, err, delay)
			time.Sleep(time.Duration(delay) * time.Second)
			delay *= 2
			if delay > 30 {
				delay = 30
			}
		}
	}

	log.Errorf("%s: 获取市场数据最终失败: %v", exchangeName, err)
	return nil, err
}

// splitSymbolsIntoBatches 将符号列表分成多个批次，每个批次包含 batchSize 个符号
// 优化：预分配切片容量
func splitSymbolsIntoBatches(symbols []string, batchSize int) [][]string {
	numBatches := (len(symbols) + batchSize - 1) / batchSize
	batches := make([][]string, 0, numBatches)

	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		// 创建切片的副本，避免共享底层数组
		batch := make([]string, end-i)
		copy(batch, symbols[i:end])
		batches = append(batches, batch)
	}

	return batches
}

// filterContractSymbols 从市场数据中筛选出符合条件的合约币种
func filterContractSymbols(markets []ccxtpro.MarketInterface, blacklist map[string]bool) []string {
	// 预分配切片容量，减少内存重新分配
	contractSymbols := make([]string, 0, len(markets)) // 预估约一半是合约

	// 优化：使用常量字符串比较，减少字符串分配
	const usdtSuffix = "/USDT:USDT"
	const usdcSuffix = "/USDC:USDC"
	const minSuffixLen = len(usdtSuffix) // 使用较短的后缀长度作为最小长度

	// 过滤出所有合约币种，只保留 */USDT:USDT 或 */USDC:USDC 格式，并应用黑名单
	for i := range markets {
		market := &markets[i]
		if market.Symbol == nil || market.Active == nil || !*market.Active {
			continue
		}

		symbol := *market.Symbol
		normalizedSymbol := strings.ToUpper(symbol)
		// 快速检查：先检查长度，再检查后缀
		if len(normalizedSymbol) < minSuffixLen {
			continue
		}
		// 检查是否匹配支持的后缀
		// 如果配置了排除 USDC:USDC，则只接受 USDT:USDT 后缀
		hasUsdtSuffix := strings.HasSuffix(normalizedSymbol, usdtSuffix)
		hasUsdcSuffix := strings.HasSuffix(normalizedSymbol, usdcSuffix)

		if !hasUsdtSuffix {
			// 如果没有 USDT 后缀，检查是否有 USDC 后缀
			if !hasUsdcSuffix {
				// 两种后缀都没有，跳过
				continue
			}
			// 有 USDC 后缀，但配置了排除 USDC:USDC，跳过
			if appConfig.ExcludeUsdcUsdcPairs {
				continue
			}
		}

		if blacklist[normalizedSymbol] {
			continue
		}

		// 从 symbol 中提取币种名称（去掉后缀）用于黑名单检查
		// 注意：这里已经确认了有USDT或USDC后缀，所以可以直接使用TrimSuffix
		var baseCurrency string
		if hasUsdtSuffix {
			baseCurrency = strings.TrimSuffix(normalizedSymbol, usdtSuffix)
		} else {
			baseCurrency = strings.TrimSuffix(normalizedSymbol, usdcSuffix)
		}

		// 合并后的黑名单检查（基于币种名称）
		if blacklist[baseCurrency] {
			continue
		}

		contractSymbols = append(contractSymbols, symbol)
	}

	return contractSymbols
}

// filterSymbolsByVolume 根据24小时成交量筛选币种
func filterSymbolsByVolume(exchangeName string, exchange ccxtpro.IExchange, symbols []string) []string {
	if len(symbols) == 0 {
		return symbols
	}

	// 批量获取 Ticker 数据
	log.Debugf("%s: 开始获取 %d 个交易对的成交量数据", exchangeName, len(symbols))
	tickers, err := exchange.FetchTickers(ccxtpro.WithFetchTickersSymbols(symbols))
	if err != nil {
		log.Warnf("%s: 获取成交量数据失败: %v，跳过成交量筛选", exchangeName, err)
		return symbols // 如果获取失败，返回所有币种
	}
	log.Debugf("%s: 成功获取 %d 个 ticker 数据", exchangeName, len(tickers.Tickers))

	filteredSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		ticker, exists := tickers.Tickers[symbol]
		if !exists {
			continue
		}

		// 检查 quoteVolume（计价货币成交量，通常是 USDT/USDC）
		var volume float64
		hasValidVolume := false

		// 优先使用 QuoteVolume，但需要检查是否为 NaN
		if ticker.QuoteVolume != nil && !math.IsNaN(*ticker.QuoteVolume) {
			volume = *ticker.QuoteVolume
			hasValidVolume = true
		} else if ticker.BaseVolume != nil && !math.IsNaN(*ticker.BaseVolume) {
			// 如果 QuoteVolume 无效，尝试使用 BaseVolume
			volume = *ticker.BaseVolume
			hasValidVolume = true
		}

		if !hasValidVolume {
			// 如果没有有效的成交量数据，跳过该币种
			continue
		}

		// 检查是否满足最小成交量要求（100万）
		if volume >= appConfig.MinVolume {
			filteredSymbols = append(filteredSymbols, symbol)
		}
	}

	log.Infof("%s: 成交量筛选完成，从 %d 个币种筛选出 %d 个（24小时成交量 >= %.0f）",
		exchangeName, len(symbols), len(filteredSymbols), appConfig.MinVolume)

	return filteredSymbols
}
