package cache

import (
	"sync"
)

var (
	cache = make(map[string]*CacheTable)
	mutex sync.RWMutex
)

func Cache(name string) *CacheTable {
	mutex.RLock()
	t, ok := cache[name]
	mutex.RUnlock()
	if !ok {
		mutex.Lock()
		t, ok = cache[name]
		if !ok {
			t = &CacheTable{
				name:  name,
				items: make(map[interface{}]*CacheItem),
			}
			cache[name] = t
		}
		mutex.Unlock()
	}
	return t
}
