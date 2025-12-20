package main

import (
	"sync"
	"time"
)

// ExchangeStatus 交易所连接状态
type ExchangeStatus struct {
	Name      string
	Connected bool
	LastSeen  time.Time
}

// ExchangeStatusManager 管理所有交易所的连接状态
type ExchangeStatusManager struct {
	mu       sync.RWMutex
	statuses map[string]*ExchangeStatus
}

// NewExchangeStatusManager 创建新的交易所状态管理器
func NewExchangeStatusManager() *ExchangeStatusManager {
	return &ExchangeStatusManager{
		statuses: make(map[string]*ExchangeStatus),
	}
}

// UpdateStatus 更新交易所状态
func (m *ExchangeStatusManager) UpdateStatus(name string, connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status, exists := m.statuses[name]
	oldConnected := false
	if exists {
		oldConnected = status.Connected
	} else {
		status = &ExchangeStatus{Name: name}
		m.statuses[name] = status
	}
	
	status.Connected = connected
	status.LastSeen = time.Now()
	
	// 记录状态变化
	if oldConnected != connected {
		if connected {
			log.Infof("交易所状态更新: %s 已连接", name)
		} else {
			log.Warnf("交易所状态更新: %s 已断开", name)
		}
	}
}

// GetStatuses 获取所有交易所状态的快照
func (m *ExchangeStatusManager) GetStatuses() []ExchangeStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]ExchangeStatus, 0, len(m.statuses))
	for _, status := range m.statuses {
		statuses = append(statuses, *status)
	}
	return statuses
}

// GetConnectedCount 获取已连接的交易所数量
func (m *ExchangeStatusManager) GetConnectedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, status := range m.statuses {
		if status.Connected {
			count++
		}
	}
	return count
}

// GetTotalCount 获取交易所总数
func (m *ExchangeStatusManager) GetTotalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.statuses)
}
