package main

import (
	"sync"
	"time"
)

// loginGuard 针对 /api/login 的按 IP 登录失败指数退避锁定。
// 与全局限流互补：全局限流约束请求速率，此处专门抬高「令牌爆破」的时间成本。
//
// 前 freeAttempts 次失败不惩罚（容忍手滑）；此后每多一次失败，锁定时长翻倍，
// 从 baseDelay 起，封顶 maxDelay。登录成功即清零。长时间无活动的记录自动衰减。
type loginGuard struct {
	mu           sync.Mutex
	entries      map[string]*loginAttempt
	lastCleanup  time.Time
	freeAttempts int
	baseDelay    time.Duration
	maxDelay     time.Duration
	decayAfter   time.Duration
}

type loginAttempt struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

const maxLoginGuardEntries = 10000

func newLoginGuard() *loginGuard {
	return &loginGuard{
		entries:      make(map[string]*loginAttempt),
		lastCleanup:  time.Now(),
		freeAttempts: 5,
		baseDelay:    time.Second,
		maxDelay:     5 * time.Minute,
		decayAfter:   15 * time.Minute,
	}
}

// allow 判断该 IP 当前是否处于锁定期。锁定时返回剩余等待时长。
func (g *loginGuard) allow(ip string, now time.Time) (bool, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked(now)

	e, ok := g.entries[ip]
	if !ok {
		return true, 0
	}
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	return true, 0
}

// recordFailure 记录一次失败并按指数退避计算下一次锁定时长。
func (g *loginGuard) recordFailure(ip string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanupLocked(now)

	e, ok := g.entries[ip]
	if !ok {
		if len(g.entries) >= maxLoginGuardEntries {
			g.evictOldestLocked()
		}
		e = &loginAttempt{}
		g.entries[ip] = e
	}
	// 距上次活动过久则视作新一轮，衰减失败计数。
	if !e.lastSeen.IsZero() && now.Sub(e.lastSeen) > g.decayAfter {
		e.failures = 0
	}
	e.failures++
	e.lastSeen = now
	if e.failures > g.freeAttempts {
		shift := uint(e.failures - g.freeAttempts - 1)
		delay := g.baseDelay
		// 逐次翻倍，注意防止移位溢出。
		for i := uint(0); i < shift && delay < g.maxDelay; i++ {
			delay *= 2
		}
		if delay > g.maxDelay {
			delay = g.maxDelay
		}
		e.lockedUntil = now.Add(delay)
	}
}

// recordSuccess 登录成功后清除该 IP 的失败记录。
func (g *loginGuard) recordSuccess(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entries, ip)
}

func (g *loginGuard) cleanupLocked(now time.Time) {
	if now.Sub(g.lastCleanup) < time.Minute {
		return
	}
	for ip, e := range g.entries {
		if now.After(e.lockedUntil) && now.Sub(e.lastSeen) > g.decayAfter {
			delete(g.entries, ip)
		}
	}
	g.lastCleanup = now
}

func (g *loginGuard) evictOldestLocked() {
	var oldestIP string
	var oldest time.Time
	for ip, e := range g.entries {
		if oldestIP == "" || e.lastSeen.Before(oldest) {
			oldestIP = ip
			oldest = e.lastSeen
		}
	}
	if oldestIP != "" {
		delete(g.entries, oldestIP)
	}
}
