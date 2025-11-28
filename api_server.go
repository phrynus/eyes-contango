package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// startAPIServer 启动API服务器
func startAPIServer(port int) {
	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("/health", handleHealth)

	// 获取所有币种数据
	mux.HandleFunc("/tickers", handleGetAllTickers)

	// 获取特定币种数据
	mux.HandleFunc("/ticker", handleGetTicker)

	// 获取最大价差
	mux.HandleFunc("/spread/max", handleGetMaxSpread)

	// 获取前N个最大价差
	mux.HandleFunc("/spread/top", handleGetTopSpreads)

	// 获取所有交易所列表
	mux.HandleFunc("/exchanges", handleGetExchanges)

	// 获取统计信息
	mux.HandleFunc("/stats", handleGetStats)

	addr := fmt.Sprintf(":%d", port)
	log.Printf("API：http://localhost%s", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("API服务器启动失败: %v", err)
	}
}

// handleHealth 健康检查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
	})
}
