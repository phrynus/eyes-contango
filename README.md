# Eyes Contango API

面向内部监控与策略使用的实时价差 API。后台抓取 Binance、Gate、Bybit、Bitget 合约报价并输出统一 JSON。
V4 是 ccxt 开源的

## 基础信息

| 项目 | 说明 |
|------|------|
| Base URL | `http://localhost:8080`（可用 `API_PORT` 覆盖） |
| 方法 | 全部端点仅支持 `GET` |
| 格式 | JSON / UTF-8 |

## 通用结构

- **TickerResponse**：单交易所行情  
  `{ "t": 1704067200000, "b": 50000.5, "bv": 1.5, "a": 50001, "av": 2 }`
- **SpreadInfo**：跨所价差  
  `{ "e": "binance-gate", "he": "binance", "le": "gate", "h": 50001, "l": 50000.5, "s": 0.5, "sp": 0.001 }`
- **Spread**：币种价差集合  
  `{ "maxs": {...}, "mins": {...}, "alls": [...], "u": 1704067200000 }`
- **CoinResponse**：单币种数据  
  `{ "s": "BTC/USDT:USDT", "e": { "binance": {...} }, "sp": {...}, "si": {...}, "u": 1704067200000 }`
- **CoinsResponse**：多币种列表  
  `{ "n": 2, "s": [ CoinResponse, ... ] }`

字段带 `omitempty`，数据缺失时直接省略。
所有时间字段 `u` 返回 Unix 毫秒级时间戳，便于前端直接比较或格式化。

## 端点速览

| 路径 | 说明 | 备注 |
|------|------|------|
| `/health` | 健康检查 | 返回状态与秒级时间戳 |
| `/tickers` | 所有币种数据 | 对象，键为 symbol |
| `/ticker?symbol=` | 单币种 | `symbol` 必填 |
| `/spread/max` | 最大价差币种 | 返回 `CoinResponse` |
| `/spread/top?n=` | 前 N 个价差 | 默认 `n=10` |
| `/exchanges` | 交易所列表 | 排序后的字符串数组 |
| `/stats` | 监控统计 | 总数、覆盖度等 |

### `/health`
```json
{ "status": "healthy", "timestamp": 1704067200 }
```

### `/tickers`
```json
{
  "BTC/USDT:USDT": {
    "s": "BTC/USDT:USDT",
    "e": { "binance": { ... }, "gate": { ... } },
    "sp": { "maxs": { ... }, "mins": { ... }, "u": 1704067200000 }
  },
  "ETH/USDT:USDT": { ... }
}
```

### `/ticker?symbol=BTC/USDT:USDT`
返回单个 `CoinResponse`。缺参数→400，未找到→404。

### `/spread/max`
```json
{
  "s": "BTC/USDT:USDT",
  "si": { "e": "binance-gate", "sp": 0.001, ... },
  "u": 1704067200000,
  "e": { "binance": { ... }, "gate": { ... } }
}
```

### `/spread/top?n=5`
```json
{
  "n": 5,
  "s": [
    { "s": "BTC/USDT:USDT", "si": { ... }, "u": 1704067200000, "e": { ... } },
    { ... }
  ]
}
```

### `/exchanges`
`["binance","bitget","bybit","gate"]`

### `/stats`
```json
{
  "totalSymbols": 150,
  "exchanges": { "binance": 120, "gate": 100, "bybit": 110, "bitget": 95 },
  "symbolsWithSpread": 145,
  "symbolsWithoutSpread": 5
}
```

## 错误与返回码

| 状态码 | 场景 |
|--------|------|
| 200 | 请求成功 |
| 400 | 参数缺失或格式错误（如 symbol 未提供） |
| 404 | 数据不存在（币种未收录、暂无价差） |
| 405 | 非 GET |
| 500 | 数据存储未就绪或内部错误 |

错误响应统一为 `text/plain`，正文直接说明原因，例如 `缺少参数: symbol`。

## 常用 cURL

```bash
curl http://localhost:8080/health
curl http://localhost:8080/tickers
curl "http://localhost:8080/ticker?symbol=BTC/USDT:USDT"
curl http://localhost:8080/spread/max
curl "http://localhost:8080/spread/top?n=5"
curl http://localhost:8080/exchanges
curl http://localhost:8080/stats
```

## 使用提示

- `/tickers` 体积大，拉全量场景再用，常规轮询建议 `/ticker`。
- symbol 统一格式 `币种/报价:结算`，示例 `BTC/USDT:USDT`。
- 价差信息依赖多交易所，数据不足时 `si` 可能为空。
- 服务器使用读写锁支持高并发读取，但仍建议合理控制频率。
- 默认监听 8080，可通过 `API_PORT` 修改。


