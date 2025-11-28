package main

import (
	"math"

	ccxt "github.com/ccxt/ccxt/go/v4"
)

// TickerResponse 可序列化的 Ticker 响应结构
type TickerResponse struct {
	Timestamp *int64   `json:"t,omitempty"`
	Bid       *float64 `json:"b,omitempty"`
	BidVolume *float64 `json:"bv,omitempty"`
	Ask       *float64 `json:"a,omitempty"`
	AskVolume *float64 `json:"av,omitempty"`
}

// CoinResponse 表示一个币种的响应数据
type CoinResponse struct {
	Symbol    string                    `json:"s"`
	Exchanges map[string]TickerResponse `json:"e"`
	Spread    Spread                    `json:"sp"`
}

// CoinsResponse 表示多个币种的响应数据
type CoinsResponse struct {
	Count int            `json:"n"`
	Coins []CoinResponse `json:"s"`
}

// safeFloat64 将 NaN 和 Inf 值转换为 nil，避免 JSON 序列化错误
func safeFloat64(f *float64) *float64 {
	if f == nil {
		return nil
	}
	if math.IsNaN(*f) || math.IsInf(*f, 0) {
		return nil
	}
	return f
}

// convertTicker 将 ccxt.Ticker 转换为 TickerResponse
func convertTicker(ticker *ccxt.Ticker) TickerResponse {
	if ticker == nil {
		return TickerResponse{}
	}
	return TickerResponse{
		Timestamp: ticker.Timestamp,
		Bid:       safeFloat64(ticker.Bid),
		BidVolume: safeFloat64(ticker.BidVolume),
		Ask:       safeFloat64(ticker.Ask),
		AskVolume: safeFloat64(ticker.AskVolume),
	}
}

// convertCoinTicker 将 CoinTicker 转换为 CoinResponse
func convertCoinTicker(coinTicker *CoinTicker) CoinResponse {
	if coinTicker == nil {
		return CoinResponse{}
	}

	exchangeMap := make(map[string]TickerResponse, len(coinTicker.Exchange))
	for name, ticker := range coinTicker.Exchange {
		if ticker != nil {
			exchangeMap[name] = convertTicker(ticker)
		}
	}

	return CoinResponse{
		Symbol:    coinTicker.Symbol,
		Exchanges: exchangeMap,
		Spread:    coinTicker.Spread,
	}
}
