package main

// $env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o contango.exe .
// $env:GOOS="linux"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build -ldflags="-s -w" -o contango .

import (
	"sync"

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
	exchanges := map[string]ccxtpro.IExchange{
		"binance":  ccxtpro.NewBinance(nil),
		"gate":     ccxtpro.NewGate(nil),
		"bybit":    ccxtpro.NewBybit(nil),
		"bitget":   ccxtpro.NewBitget(nil),
		"backpack": ccxtpro.NewBackpack(nil),
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

	go startAPIServer(appConfig.APIPort)

	wg.Wait()
}
