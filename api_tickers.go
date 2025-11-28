package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// handleGetAllTickers 获取所有币种数据
func handleGetAllTickers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if globalTickers == nil {
		http.Error(w, "数据存储未初始化", http.StatusInternalServerError)
		return
	}

	globalTickers.mu.RLock()
	tickers := make(map[string]CoinResponse, len(globalTickers.data))
	for k, v := range globalTickers.data {
		if v != nil && len(v.Exchange) > 0 {
			tickers[k] = convertCoinTicker(v)
		}
	}
	globalTickers.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(tickers); err != nil {
		log.Printf("ERROR: JSON编码失败: %v", err)
		http.Error(w, "JSON编码失败", http.StatusInternalServerError)
	}
}

// handleGetTicker 获取特定币种数据
func handleGetTicker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "缺少参数: symbol", http.StatusBadRequest)
		return
	}

	globalTickers.mu.RLock()
	coinTicker := globalTickers.data[symbol]
	globalTickers.mu.RUnlock()

	if coinTicker == nil {
		http.Error(w, fmt.Sprintf("币种 %s 不存在", symbol), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(convertCoinTicker(coinTicker)); err != nil {
		log.Printf("ERROR: JSON编码失败: %v", err)
		http.Error(w, "JSON编码失败", http.StatusInternalServerError)
	}
}
