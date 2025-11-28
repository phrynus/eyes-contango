package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
)

// handleGetMaxSpread 获取最大价差
func handleGetMaxSpread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	globalTickers.mu.RLock()
	var maxSpreadInfo *SpreadInfo
	var maxSpreadCoinTicker *CoinTicker

	for _, coinTicker := range globalTickers.data {
		if coinTicker == nil || coinTicker.Spread.MaxSpread == nil {
			continue
		}

		if maxSpreadInfo == nil || coinTicker.Spread.MaxSpread.SpreadPercent > maxSpreadInfo.SpreadPercent {
			maxSpreadInfo = coinTicker.Spread.MaxSpread
			maxSpreadCoinTicker = coinTicker
		}
	}
	globalTickers.mu.RUnlock()

	if maxSpreadInfo == nil {
		http.Error(w, "暂无有效的价差数据", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	response := convertCoinTicker(maxSpreadCoinTicker)
	encoder.Encode(response)
}

// handleGetTopSpreads 获取前N个最大价差
func handleGetTopSpreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	n := 10
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsedN, err := strconv.Atoi(nStr); err == nil && parsedN > 0 {
			n = parsedN
		}
	}

	type SpreadItem struct {
		Symbol     string
		Spread     *SpreadInfo
		CoinTicker *CoinTicker
	}

	globalTickers.mu.RLock()
	spreadItems := make([]SpreadItem, 0, len(globalTickers.data))
	for symbol, coinTicker := range globalTickers.data {
		if coinTicker != nil && coinTicker.Spread.MaxSpread != nil {
			spreadItems = append(spreadItems, SpreadItem{
				Symbol:     symbol,
				Spread:     coinTicker.Spread.MaxSpread,
				CoinTicker: coinTicker,
			})
		}
	}
	globalTickers.mu.RUnlock()

	sort.Slice(spreadItems, func(i, j int) bool {
		return spreadItems[i].Spread.SpreadPercent > spreadItems[j].Spread.SpreadPercent
	})

	if n > len(spreadItems) {
		n = len(spreadItems)
	}

	topSpreads := make([]CoinResponse, 0, n)
	for i := 0; i < n; i++ {
		item := spreadItems[i]
		coinResp := convertCoinTicker(item.CoinTicker)
		topSpreads = append(topSpreads, coinResp)
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.Encode(CoinsResponse{
		Count: n,
		Coins: topSpreads,
	})
}
