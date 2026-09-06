package main

import "time"

// evictionSampleSize 采样淘汰时随机抽样的条数（Go map 遍历本身随机，
// 从头取 N 条即等价于随机采样）。
const evictionSampleSize = 16

// evictOldestSampled 以近似 LRU 的方式从 m 中淘汰一条“最旧”记录：
// 随机采样 sample 条，删除其中 lastSeen 最小的一条并返回是否删除成功。
// 万条上限下全表扫描淘汰会让每次插入退化为 O(n)；采样淘汰把插入成本
// 降为近似 O(1)，对“谁先被挤出”不敏感的场景（缓存/限流/短链/锁定）足够。
func evictOldestSampled[K comparable, V any](m map[K]V, lastSeen func(V) time.Time, sample int) bool {
	if len(m) == 0 {
		return false
	}
	if sample > len(m) {
		sample = len(m)
	}
	var (
		oldestKey  K
		oldestAt   time.Time
		haveOldest bool
	)
	i := 0
	for key, value := range m {
		at := lastSeen(value)
		if !haveOldest || at.Before(oldestAt) {
			oldestKey = key
			oldestAt = at
			haveOldest = true
		}
		i++
		if i >= sample {
			break
		}
	}
	if !haveOldest {
		return false
	}
	delete(m, oldestKey)
	return true
}
