package main

import (
	"strings"
	"sync"
	"time"

	// ccxt "github.com/ccxt/ccxt/go/v4"
	ccxtpro "github.com/ccxt/ccxt/go/v4/pro"
)

// watchExchange 为单个交易所处理订阅逻辑
func watchExchange(exchangeName string, exchange ccxtpro.IExchange, blacklist map[string]bool, wg *sync.WaitGroup) {
	defer wg.Done()

	// 初始化交易所状态为未连接
	globalExchanges.UpdateStatus(exchangeName, false)

	// 用于存储当前订阅的批次 goroutine，以便在重新筛选时停止旧的订阅
	var currentBatches []func() // 存储停止函数
	var batchesMutex sync.Mutex

	// 启动订阅的函数
	startWatching := func(symbols []string) {
		if len(symbols) == 0 {
			// log.Printf("%s: No contract symbols found\n", exchangeName)
			return
		}

		// 将符号列表分成批次（预分配容量）
		batches := splitSymbolsIntoBatches(symbols, appConfig.BatchSize)

		// 为每个批次创建一个 goroutine 来订阅
		var batchWg sync.WaitGroup
		batchWg.Add(len(batches))

		stopChannels := make([]chan bool, len(batches))

		for i, batch := range batches {
			stopChan := make(chan bool, 1)
			stopChannels[i] = stopChan

			go func(batchNum int, batchSymbols []string, stopCh chan bool) {
				defer batchWg.Done()
				watchBatchWithStop(exchangeName, exchange, batchNum, batchSymbols, stopCh)
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
		// 获取所有市场（带重试机制）
		markets, err := fetchMarketsWithRetry(exchange, exchangeName)
		if err != nil {
			// log.Printf("%s: Failed to fetch markets after retries: %v\n", exchangeName, err)
			return
		}

		// 先筛选出符合条件的币种（格式和黑名单）
		contractSymbols := filterContractSymbols(markets, blacklist)

		// 根据成交量进一步筛选
		contractSymbols = filterSymbolsByVolume(exchangeName, exchange, contractSymbols)

		// 停止旧的订阅
		batchesMutex.Lock()
		for _, stop := range currentBatches {
			if stop != nil {
				stop()
			}
		}
		currentBatches = nil
		batchesMutex.Unlock()

		// 等待一小段时间，确保旧的订阅已停止
		time.Sleep(2 * time.Second)

		// 启动新的订阅
		startWatching(contractSymbols)
	}

	// 立即执行一次筛选和订阅
	filterAndStart()

	// 每小时重新筛选一次
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		// log.Printf("%s: 开始每小时重新筛选币种...\n", exchangeName)
		filterAndStart()
	}
}

// watchBatchWithStop 处理单个批次的订阅，带重试机制和停止信号
// 优化：使用带停止信号的sleep，避免忙等待，降低CPU占用
func watchBatchWithStop(exchangeName string, exchange ccxtpro.IExchange, batchNum int, symbols []string, stopCh chan bool) {

	retryCount := 0
	delay := appConfig.InitialRetryDelay

	for {
		// 检查是否需要停止（非阻塞）
		if stopCh != nil {
			select {
			case <-stopCh:
				// log.Printf("%s - Batch %d: 收到停止信号，退出订阅\n", exchangeName, batchNum)
				return
			default:
			}
		}

		bidsAsks, err := exchange.WatchBidsAsks(
			ccxtpro.WithWatchBidsAsksSymbols(symbols),
		)
		if err != nil {
			// 更新为未连接状态
			globalExchanges.UpdateStatus(exchangeName, false)
			// 再次检查停止信号
			if stopCh != nil {
				select {
				case <-stopCh:
					// log.Printf("%s - Batch %d: 收到停止信号，退出订阅\n", exchangeName, batchNum)
					return
				default:
				}
			}

			retryCount++
			if retryCount <= appConfig.MaxRetries {
				// log.Printf("%s - Batch %d: Error (retry %d/%d): %v, retrying in %ds\n",
				// 	exchangeName, batchNum, retryCount, appConfig.MaxRetries, err, delay)

				// 使用带停止信号的sleep，避免忙等待
				if stopCh != nil {
					select {
					case <-stopCh:
						// log.Printf("%s - Batch %d: 收到停止信号，退出订阅\n", exchangeName, batchNum)
						return
					case <-time.After(time.Duration(delay) * time.Second):
						// 继续重试
					}
				} else {
					time.Sleep(time.Duration(delay) * time.Second)
				}

				// 指数退避，但限制最大延迟为30秒
				delay *= 2
				if delay > 30 {
					delay = 30
				}
				continue
			} else {
				// log.Printf("%s - Batch %d: Max retries reached, stopping\n", exchangeName, batchNum)
				return
			}
		}

		// 成功连接后重置重试计数和延迟
		retryCount = 0
		delay = appConfig.InitialRetryDelay
		// 更新为已连接状态
		globalExchanges.UpdateStatus(exchangeName, true)

		// 处理接收到的数据并实时更新到存储
		if len(bidsAsks.Tickers) > 0 {
			updateTickers(exchangeName, bidsAsks.Tickers)
		}
	}
}

// fetchMarketsWithRetry 带重试机制获取市场数据
func fetchMarketsWithRetry(exchange ccxtpro.IExchange, exchangeName string) ([]ccxtpro.MarketInterface, error) {
	var markets []ccxtpro.MarketInterface
	var err error
	delay := appConfig.InitialRetryDelay

	for i := 0; i < appConfig.MaxRetries; i++ {
		markets, err = exchange.FetchMarkets()
		if err == nil {
			return markets, nil
		}

		if i < appConfig.MaxRetries-1 {
			// log.Printf("%s: Error fetching markets (attempt %d/%d): %v, retrying in %ds\n",
			// 	exchangeName, i+1, appConfig.MaxRetries, err, delay)
			time.Sleep(time.Duration(delay) * time.Second)
			delay *= 2
			if delay > 30 {
				delay = 30
			}
		}
	}

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
	contractSymbols := make([]string, 0, len(markets)/2) // 预估约一半是合约

	// 优化：使用常量字符串比较，减少字符串分配
	const usdtSuffix = "/USDT:USDT"
	const usdcSuffix = "/USDC:USDC"
	const minSuffixLen = len(usdcSuffix) // 使用较短的后缀长度作为最小长度

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
		if !strings.HasSuffix(normalizedSymbol, usdtSuffix) && !strings.HasSuffix(normalizedSymbol, usdcSuffix) {
			continue
		}

		if blacklist[normalizedSymbol] {
			continue
		}

		// 从 symbol 中提取币种名称（去掉后缀）用于黑名单检查
		var baseCurrency string
		if strings.HasSuffix(normalizedSymbol, usdtSuffix) {
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
	tickers, err := exchange.FetchTickers(ccxtpro.WithFetchTickersSymbols(symbols))
	if err != nil {
		// log.Printf("%s: 获取成交量数据失败: %v，跳过成交量筛选\n", exchangeName, err)
		return symbols // 如果获取失败，返回所有币种
	}

	filteredSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		ticker, exists := tickers.Tickers[symbol]
		if !exists {
			continue
		}

		// 检查 quoteVolume（计价货币成交量，通常是 USDT/USDC）
		var volume float64
		if ticker.QuoteVolume != nil {
			volume = *ticker.QuoteVolume
		} else if ticker.BaseVolume != nil {
			// 如果没有 quoteVolume，尝试使用 baseVolume
			volume = *ticker.BaseVolume
		} else {
			// 如果没有成交量数据，跳过该币种
			continue
		}

		// 检查是否满足最小成交量要求（100万）
		if volume >= appConfig.MinVolume {
			filteredSymbols = append(filteredSymbols, symbol)
		}
	}

	// log.Printf("%s: 成交量筛选完成，从 %d 个币种筛选出 %d 个（24小时成交量 >= %.0f）\n",
	// 	exchangeName, len(symbols), len(filteredSymbols), appConfig.MinVolume)

	return filteredSymbols
}
