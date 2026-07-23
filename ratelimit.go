package main

import (
	"sync"
	"time"
)

// RateLimiter 简单的令牌桶频率限制器（按IP）
type RateLimiter struct {
	mu          sync.Mutex
	clients     map[string]*clientState
	rate        int // 每秒允许的请求数
	interval    time.Duration
	lastCleanup time.Time
}

type clientState struct {
	tokens   int
	lastSeen time.Time
}

func newRateLimiter(ratePerSecond int) *RateLimiter {
	if ratePerSecond < 1 {
		ratePerSecond = 1
	}
	return &RateLimiter{
		clients:     make(map[string]*clientState),
		rate:        ratePerSecond,
		interval:    time.Second,
		lastCleanup: time.Now(),
	}
}

const maxRateLimiterClients = 10000

// Allow 判断该IP是否被允许访问
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cs, ok := rl.clients[ip]
	now := time.Now()
	if now.Sub(rl.lastCleanup) >= time.Minute {
		for clientIP, state := range rl.clients {
			if now.Sub(state.lastSeen) > 5*time.Minute {
				delete(rl.clients, clientIP)
			}
		}
		rl.lastCleanup = now
	}
	if !ok {
		if len(rl.clients) >= maxRateLimiterClients {
			var oldestIP string
			var oldest time.Time
			for clientIP, state := range rl.clients {
				if oldestIP == "" || state.lastSeen.Before(oldest) {
					oldestIP = clientIP
					oldest = state.lastSeen
				}
			}
			if oldestIP != "" {
				delete(rl.clients, oldestIP)
			}
		}
		rl.clients[ip] = &clientState{tokens: rl.rate - 1, lastSeen: now}
		return true
	}

	elapsed := now.Sub(cs.lastSeen)
	refill := int(elapsed / rl.interval)
	if refill > 0 {
		cs.tokens += refill * rl.rate
		if cs.tokens > rl.rate {
			cs.tokens = rl.rate
		}
		cs.lastSeen = now
	}

	if cs.tokens <= 0 {
		return false
	}
	cs.tokens--
	return true
}
