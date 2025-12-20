package main

// $env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o contango.exe .
// $env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o contango .

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	ccxtpro "github.com/ccxt/ccxt/go/v4/pro"
	"github.com/phrynus/go-utils/logger"
)

var log *logger.Logger

func init() {
	// 先初始化日志记录器（使用默认配置，因为配置还未加载）
	var err error
	log, err = logger.NewLogger(logger.LogConfig{
		Filename: "main.log", // log filename
		LogDir:   "logs",     // log directory
		MaxSize:  10 * 1024,  // KB
		StdoutLevels: map[int]bool{
			logger.INFO:  false,
			logger.DEBUG: false,
			logger.WARN:  false,
			logger.ERROR: false,
		},
		ColorOutput:  true,
		ShowFileLine: true,
	})
	if err != nil {
		panic(err)
	}

	loadConfig()

	for i := 0; i < appConfig.SpreadCalcWorkers; i++ {
		globalTickers.spreadCalcWg.Add(1)
		go globalTickers.spreadCalcWorker(i)
	}

	// 启动状态日志记录器，每分钟记录一次整体状态
	globalTickers.startStatusLogger()
}

func main() {

	// 使用 defer 确保程序退出时关闭日志
	defer func() {
		if err := log.Close(); err != nil {
			fmt.Printf("关闭日志记录器失败: %v\n", err)
		}
	}()

	if appConfig.TableLimit < 0 {
		log.Error("表格限制不能小于 0")
		os.Exit(1)
	}

	refreshInterval := time.Duration(appConfig.RefreshIntervalMs) * time.Millisecond
	if refreshInterval < 50*time.Millisecond {
		refreshInterval = 50 * time.Millisecond
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	watchersWg := launchExchangeWatchers()
	go func() {
		watchersWg.Wait()
		stop()
	}()

	runSpreadTable(ctx, appConfig.TableLimit, refreshInterval, appConfig.MinSpreadPercent)
}

func launchExchangeWatchers() *sync.WaitGroup {
	// 合约模式配置
	contractConfig := map[string]interface{}{
		"options": map[string]interface{}{
			"defaultType": "swap",
		},
	}

	allExchanges := map[string]ccxtpro.IExchange{
		"binance":  ccxtpro.NewBinance(contractConfig),
		"okx":      ccxtpro.NewOkx(contractConfig),
		"bybit":    ccxtpro.NewBybit(contractConfig),
		"kucoin":   ccxtpro.NewKucoinfutures(contractConfig),
		"gate":     ccxtpro.NewGate(contractConfig),
		"bitget":   ccxtpro.NewBitget(contractConfig),
		"backpack": ccxtpro.NewBackpack(contractConfig),
	}

	// 根据配置选择实际启用的交易所
	enabled := appConfig.EnabledExchanges
	if len(enabled) == 0 {
		log.Warn("未在配置中指定 enabledExchanges，将不会启动任何交易所")
	}

	exchanges := make(map[string]ccxtpro.IExchange, len(enabled))
	for _, name := range enabled {
		if ex, ok := allExchanges[name]; ok {
			exchanges[name] = ex
		} else {
			log.Warnf("配置的交易所 %q 未在程序中实现，已忽略", name)
		}
	}

	if len(exchanges) == 0 {
		log.Error("启用的交易所列表为空")
		return nil
	}

	commonDefaults := defaultCommonBlacklist
	if configFromFile != nil {
		commonDefaults = fallbackSlice(configFromFile.CommonBlacklist, defaultCommonBlacklist)
	}
	commonBlacklist := normalizeBlacklistEntries(commonDefaults)

	exchangeBlacklists := make(map[string]map[string]bool, len(exchanges))
	for name := range exchanges {
		var exchangeDefaults []string
		if configFromFile != nil && configFromFile.ExchangeBlacklists != nil {
			exchangeDefaults = configFromFile.ExchangeBlacklists[name]
		}
		exchangeBlacklists[name] = normalizeBlacklistEntries(exchangeDefaults)
	}

	var wg sync.WaitGroup
	wg.Add(len(exchanges))

	// 添加启动延迟，避免所有交易所同时启动导致资源竞争
	startDelay := 500 * time.Millisecond
	idx := 0
	log.Infof("准备启动 %d 个交易所监听器，每个延迟 %v", len(exchanges), startDelay)
	for name, exchange := range exchanges {
		mergedBlacklist := mergeBlacklists(commonBlacklist, exchangeBlacklists[name])
		blacklistCount := len(mergedBlacklist)
		// 为每个交易所添加递增的延迟，避免同时启动
		delay := startDelay * time.Duration(idx)
		go func(exName string, ex ccxtpro.IExchange, bl map[string]bool, d time.Duration, blCount int) {
			if d > 0 {
				log.Debugf("交易所 %s 等待 %v 后启动", exName, d)
				time.Sleep(d)
			}
			log.Infof("启动交易所监听器: %s (黑名单: %d 项)", exName, blCount)
			watchExchange(exName, ex, bl, &wg)
		}(name, exchange, mergedBlacklist, delay, blacklistCount)
		idx++
	}

	log.Infof("已启动 %d 个交易所监听器（延迟启动以避免资源竞争）", len(exchanges))
	return &wg
}
