package main

import (
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultConfigFile          = "config.yaml"
	defaultBatchSize           = 50
	defaultMaxRetries          = 5
	defaultInitialRetryDelay   = 1
	defaultTickerValidity      = int64(10000)
	defaultMinVolume           = 500000.0
	defaultSpreadCalcWorkers   = 8
	defaultSpreadCalcQueueSize = 1000
	defaultSpreadCalcThrottle  = int64(500)
	defaultAPIPort             = 8080
)

type runtimeConfig struct {
	BatchSize            int
	MaxRetries           int
	InitialRetryDelay    int
	TickerValidity       int64
	MinVolume            float64
	SpreadCalcWorkers    int
	SpreadCalcQueueSize  int
	SpreadCalcThrottleMs int64
	APIPort              int
}

type fileConfig struct {
	BatchSize            *int                `yaml:"batchSize"`
	MaxRetries           *int                `yaml:"maxRetries"`
	InitialRetryDelay    *int                `yaml:"initialRetryDelay"`
	TickerValidity       *int64              `yaml:"tickerValidity"`
	MinVolume            *float64            `yaml:"minVolume"`
	SpreadCalcWorkers    *int                `yaml:"spreadCalcWorkers"`
	SpreadCalcQueueSize  *int                `yaml:"spreadCalcQueueSize"`
	SpreadCalcThrottleMs *int64              `yaml:"spreadCalcThrottleMs"`
	APIPort              *int                `yaml:"apiPort"`
	CommonBlacklist      []string            `yaml:"commonBlacklist"`
	ExchangeBlacklists   map[string][]string `yaml:"exchangeBlacklists"`
}

var (
	configFromFile *fileConfig
	appConfig      runtimeConfig
	globalTickers  *CoinTickers
)

// 加载配置 - 仅使用 YAML
func loadConfig() {
	configPath := defaultConfigFile
	if len(os.Args) > 1 {
		if trimmed := strings.TrimSpace(os.Args[1]); trimmed != "" {
			configPath = trimmed
		}
	}

	cfg, err := loadFileConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("提示: 未找到配置文件 %s，使用内置默认值", configPath)
		} else {
			log.Printf("警告: 加载配置文件 %s 失败: %v，将使用内置默认值", configPath, err)
		}
		cfg = &fileConfig{}
	}
	configFromFile = cfg

	// 读取配置项，提供默认值
	appConfig = runtimeConfig{
		BatchSize:            valueOrDefault(cfg.BatchSize, defaultBatchSize),
		MaxRetries:           valueOrDefault(cfg.MaxRetries, defaultMaxRetries),
		InitialRetryDelay:    valueOrDefault(cfg.InitialRetryDelay, defaultInitialRetryDelay),
		TickerValidity:       valueOrDefault(cfg.TickerValidity, defaultTickerValidity),
		MinVolume:            valueOrDefault(cfg.MinVolume, defaultMinVolume),
		SpreadCalcWorkers:    valueOrDefault(cfg.SpreadCalcWorkers, defaultSpreadCalcWorkers),
		SpreadCalcQueueSize:  valueOrDefault(cfg.SpreadCalcQueueSize, defaultSpreadCalcQueueSize),
		SpreadCalcThrottleMs: valueOrDefault(cfg.SpreadCalcThrottleMs, defaultSpreadCalcThrottle),
		APIPort:              valueOrDefault(cfg.APIPort, defaultAPIPort),
	}

	// 初始化全局数据存储（需要配置值）
	globalTickers = &CoinTickers{
		data:           make(map[string]*CoinTicker),
		spreadCalcChan: make(chan string, appConfig.SpreadCalcQueueSize),
		lastCalcTime:   make(map[string]int64),
	}

	log.Printf("配置加载完成 (config=%s): batchSize=%d, maxRetries=%d, tickerValidity=%dms, minVolume=%.0f, spreadCalcWorkers=%d, spreadCalcQueueSize=%d, spreadCalcThrottleMs=%d, apiPort=%d",
		configPath,
		appConfig.BatchSize,
		appConfig.MaxRetries,
		appConfig.TickerValidity,
		appConfig.MinVolume,
		appConfig.SpreadCalcWorkers,
		appConfig.SpreadCalcQueueSize,
		appConfig.SpreadCalcThrottleMs,
		appConfig.APIPort,
	)
}

func valueOrDefault[T any](ptr *T, defaultValue T) T {
	if ptr != nil {
		return *ptr
	}
	return defaultValue
}

func loadFileConfig(path string) (*fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg fileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
