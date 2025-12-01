package main

// $env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o contango.exe .
// $env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o contango .

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	ccxtpro "github.com/ccxt/ccxt/go/v4/pro"
)

func init() {
	loadConfig()

	for i := 0; i < appConfig.SpreadCalcWorkers; i++ {
		globalTickers.spreadCalcWg.Add(1)
		go globalTickers.spreadCalcWorker(i)
	}
}

func main() {
	if appConfig.TableLimit < 0 {
		log.Fatalf("表格限制不能小于 0")
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
	exchanges := map[string]ccxtpro.IExchange{
		"binance": ccxtpro.NewBinance(nil),
		"gate":    ccxtpro.NewGate(nil),
		"bybit":   ccxtpro.NewBybit(nil),
		"bitget":  ccxtpro.NewBitget(nil),
		// "backpack": ccxtpro.NewBackpack(nil),
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

	for name, exchange := range exchanges {
		mergedBlacklist := mergeBlacklists(commonBlacklist, exchangeBlacklists[name])
		go watchExchange(name, exchange, mergedBlacklist, &wg)
	}

	return &wg
}
