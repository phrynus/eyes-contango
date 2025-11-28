package main

import "strings"

var defaultCommonBlacklist = []string{"BTC", "ETH", "SOL"}
var supportedQuoteCurrencies = []string{"USDT", "USDC"}

func normalizeBlacklistEntries(symbols []string) map[string]bool {
	if len(symbols) == 0 {
		return make(map[string]bool)
	}

	blacklist := make(map[string]bool, len(symbols))
	for _, symbol := range symbols {
		addBlacklistEntry(blacklist, symbol)
	}
	return blacklist
}

func addBlacklistEntry(blacklist map[string]bool, symbol string) {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return
	}

	blacklist[normalized] = true

	if pairKey := extractPairKey(normalized); pairKey != "" {
		blacklist[pairKey] = true
	}

	if base := extractBaseCurrency(normalized); base != "" {
		blacklist[base] = true
	}
}

func extractBaseCurrency(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}

	if strings.Contains(normalized, "/") {
		parts := strings.SplitN(normalized, "/", 2)
		base := strings.TrimSpace(parts[0])
		base = strings.TrimSuffix(base, ":")
		return base
	}

	temp := normalized
	if idx := strings.Index(temp, ":"); idx != -1 {
		temp = temp[:idx]
	}

	for _, suffix := range supportedQuoteCurrencies {
		if strings.HasSuffix(temp, suffix) && len(temp) > len(suffix) {
			return strings.TrimSpace(temp[:len(temp)-len(suffix)])
		}
	}

	return strings.TrimSpace(temp)
}

func extractPairKey(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}

	if strings.Contains(normalized, "/") {
		parts := strings.SplitN(normalized, "/", 2)
		base := strings.TrimSpace(parts[0])
		quotePart := strings.TrimSpace(parts[1])
		if idx := strings.Index(quotePart, ":"); idx != -1 {
			quotePart = quotePart[:idx]
		}
		if quotePart == "" || base == "" {
			return ""
		}
		return base + quotePart
	}

	temp := normalized
	if idx := strings.Index(temp, ":"); idx != -1 {
		temp = temp[:idx]
	}

	for _, quote := range supportedQuoteCurrencies {
		if strings.HasSuffix(temp, quote) && len(temp) > len(quote) {
			base := strings.TrimSpace(temp[:len(temp)-len(quote)])
			if base == "" {
				return ""
			}
			return base + quote
		}
	}

	return ""
}

func fallbackSlice(values []string, fallback []string) []string {
	if len(values) == 0 {
		return fallback
	}
	return values
}

// mergeBlacklists 合并共同黑名单和交易所独立黑名单，返回一个合并后的map
func mergeBlacklists(common, exchange map[string]bool) map[string]bool {
	merged := make(map[string]bool, len(common)+len(exchange))
	for k, v := range common {
		merged[k] = v
	}
	for k, v := range exchange {
		merged[k] = v
	}
	return merged
}
