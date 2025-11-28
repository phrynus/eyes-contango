package main

import (
	"encoding/json"
	"net/http"
	"sort"
)

// handleGetExchanges 获取所有交易所列表
func handleGetExchanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	globalTickers.mu.RLock()
	exchangeSet := make(map[string]bool)
	for _, coinTicker := range globalTickers.data {
		if coinTicker != nil {
			for exchangeName := range coinTicker.Exchange {
				exchangeSet[exchangeName] = true
			}
		}
	}
	globalTickers.mu.RUnlock()

	exchanges := make([]string, 0, len(exchangeSet))
	for exchangeName := range exchangeSet {
		exchanges = append(exchanges, exchangeName)
	}

	sort.Strings(exchanges)

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.Encode(exchanges)
}

// handleGetStats 获取统计信息
func handleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	globalTickers.mu.RLock()
	exchangeCounts := make(map[string]int)
	symbolsWithSpread := 0
	symbolsWithoutSpread := 0

	for _, coinTicker := range globalTickers.data {
		if coinTicker == nil {
			continue
		}
		for exchangeName := range coinTicker.Exchange {
			exchangeCounts[exchangeName]++
		}

		if coinTicker.Spread.MaxSpread != nil {
			symbolsWithSpread++
		} else {
			symbolsWithoutSpread++
		}
	}
	totalSymbols := len(globalTickers.data)
	globalTickers.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.Encode(map[string]interface{}{
		"totalSymbols":         totalSymbols,
		"exchanges":            exchangeCounts,
		"symbolsWithSpread":    symbolsWithSpread,
		"symbolsWithoutSpread": symbolsWithoutSpread,
	})
}
