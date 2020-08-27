package cache

import (
	"log"
	"sort"
	"sync"
	"time"
)

type CacheTable struct {
	sync.RWMutex

	name  string
	items map[interface{}]*CacheItem

	cleanupTimer    *time.Timer
	cleanupInterval time.Duration

	logger   *log.Logger
	loadData func(key interface{}, args ...interface{}) *CacheItem

	addItem []func(item *CacheItem)

	aboutToDeleteItem []func(item *CacheItem)
}

// 表长度
func (table *CacheTable) Count() int {
	table.RLock()
	defer table.RUnlock()
	return len(table.items)
}

// 遍历所有元素
func (table *CacheTable) Foreach(trans func(key interface{}, item *CacheItem)) {
	table.RLock()
	defer table.RUnlock()

	for k, v := range table.items {
		trans(k, v)
	}
}

// 数据加载
func (table *CacheTable) SetDataLoader(f func(interface{}, ...interface{}) *CacheItem) {
	table.RLock()
	defer table.RUnlock()
	table.loadData = f
}

// 设置Callback
func (table *CacheTable) SetAddedItemCallback(f func(item *CacheItem)) {
	if len(table.addItem) > 0 {
		table.RemoveAddedItemCallbacks()
	}
	table.AddAddedItemCallback(f)
}

// 添加Callback
func (table *CacheTable) AddAddedItemCallback(f func(item *CacheItem)) {
	table.RLock()
	defer table.RUnlock()
	table.addItem = append(table.addItem, f)
}

// RemoveAddedItemCallbacks 置空callback
func (table *CacheTable) RemoveAddedItemCallbacks() {
	table.Lock()
	defer table.Unlock()
	table.addItem = nil
}

// SetAboutToDeleteItemCallback 设置一个aboutToDeleteItem，并返回所有
func (table *CacheTable) SetAboutToDeleteItemCallback(f func(*CacheItem)) {
	if len(table.aboutToDeleteItem) > 0 {
		table.RemoveAboutToDeleteItemCallback()
	}
	table.Lock()
	defer table.Unlock()
	table.aboutToDeleteItem = append(table.aboutToDeleteItem, f)
}

// AddAboutToDeleteItemCallback 追加aboutToDeleteItem
func (table *CacheTable) AddAboutToDeleteItemCallback(f func(*CacheItem)) {
	table.Lock()
	defer table.Unlock()
	table.aboutToDeleteItem = append(table.aboutToDeleteItem, f)
}

// RemoveAboutToDeleteItemCallback 移除aboutToDeleteItem
func (table *CacheTable) RemoveAboutToDeleteItemCallback() {
	table.Lock()
	defer table.Unlock()
	table.aboutToDeleteItem = nil
}

func (table *CacheTable) expirationCheck() {
	table.Lock()
	if table.cleanupTimer != nil {
		table.cleanupTimer.Stop()
	}
	if table.cleanupInterval > 0 {
		table.log("Expiration check triggered after", table.cleanupInterval, "for table", table.name)
	} else {
		table.log("Expiration check installed for table", table.name)
	}
	now := time.Now()
	smallestDuration := 0 * time.Second

	for key, item := range table.items {
		item.RLock()
		// 存活时长(有效期)
		lifeSpan := item.lifeSpan
		/// 生效时间
		assessedOn := item.accessedOn
		item.RUnlock()

		if lifeSpan == 0 {
			continue
		}
		if now.Sub(assessedOn) >= lifeSpan {
			// 已失效
			table.deleteInternal(key)
		} else {
			if smallestDuration == 0 || lifeSpan-now.Sub(assessedOn) < smallestDuration {
				smallestDuration = lifeSpan - now.Sub(assessedOn)
			}
		}
	}

	table.cleanupInterval = smallestDuration
	if smallestDuration > 0 {
		// 定时递归检测是否是失效
		table.cleanupTimer = time.AfterFunc(smallestDuration, func() {
			// 通过协程重检测
			go table.expirationCheck()
		})
	}
	table.Unlock()
}

func (table *CacheTable) addInternal(item *CacheItem) {
	// Careful: do not run this method unless the table-mutex is locked!
	// It will unlock it for the caller before running the callbacks and checks
	table.log("Adding item with key", item.key, "and lifespan of", item.lifeSpan, "to table", table.name)
	table.items[item.key] = item

	// Cache values so we don't keep blocking the mutex.
	expDur := table.cleanupInterval
	addedItem := table.addItem
	table.Unlock()

	// Trigger callback after adding an item to cache.
	if addedItem != nil {
		for _, callback := range addedItem {
			callback(item)
		}
	}

	// If we haven't set up any expiration check timer or found a more imminent item.
	if item.lifeSpan > 0 && (expDur == 0 || item.lifeSpan < expDur) {
		table.expirationCheck()
	}
}

// Add 添加键值对到table
func (table *CacheTable) Add(key interface{}, lifeSpan time.Duration, data interface{}) *CacheItem {
	item := NewCacheItem(key, lifeSpan, data)

	// Add item to cache.
	table.Lock()
	table.addInternal(item)

	return item
}

// 移除元素
func (table *CacheTable) deleteInternal(key interface{}) (*CacheItem, error) {
	r, ok := table.items[key]
	if !ok {
		return nil, ErrKeyNotFound
	}
	aboutToDeleteItem := table.aboutToDeleteItem
	table.Unlock()
	if aboutToDeleteItem != nil {
		for _, callback := range aboutToDeleteItem {
			callback(r)
		}
	}

	r.RLock()
	defer r.Unlock()
	if r.aboutToExpire != nil {
		for _, callback := range r.aboutToExpire {
			callback(key)
		}
	}

	table.Lock()
	table.log("Deleting item with key", key, "created on", r.createdOn, "and hit", r.accessCount, "times from table", table.name)
	delete(table.items, key)
	return r, nil
}

// 公共移除元素方法
func (table *CacheTable) Delete(key interface{}) (*CacheItem, error) {
	table.Lock()
	defer table.Unlock()
	delete(table.items, key)
	return table.deleteInternal(key)
}

// 是否存在key元素
func (table *CacheTable) Exists(key interface{}) bool {
	table.RLock()
	defer table.RUnlock()
	_, ok := table.items[key]
	return ok
}

func (table *CacheTable) NotFoundAdd(key interface{}, lifeSpan time.Duration, data interface{}) bool {
	table.Lock()

	if _, ok := table.items[key]; ok {
		table.Unlock()
		return false
	}
	item := NewCacheItem(key, lifeSpan, data)
	table.addInternal(item)
	return true
}

func (table *CacheTable) Value(key interface{}, args ...interface{}) (*CacheItem, error) {
	table.RLock()
	item, ok := table.items[key]
	loadData := table.loadData

	table.RUnlock()
	if ok {
		item.KeepAlive()
		return item, nil
	}
	if loadData != nil {
		item = loadData(key, args)
		if item != nil {
			table.Add(key, item.lifeSpan, item)
			return item, nil
		}
		return nil, ErrKeyNotFoundOrLoadable
	}
	return nil, ErrKeyNotFound
}

func (table *CacheTable) Flush() {
	table.Lock()
	defer table.Unlock()
	table.log("Flushing table", table.name)

	table.items = make(map[interface{}]*CacheItem)
	table.cleanupInterval = 0
	if table.cleanupTimer != nil {
		table.cleanupTimer.Stop()
	}
}

type CacheItemPair struct {
	Key         interface{}
	AccessCount int64
}

type CacheItemPairList []CacheItemPair

func (p CacheItemPairList) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p CacheItemPairList) Len() int           { return len(p) }
func (p CacheItemPairList) Less(i, j int) bool { return p[i].AccessCount > p[j].AccessCount }

func (table *CacheTable) MostAccessed(count int64) []*CacheItem {
	table.RLock()
	defer table.Unlock()

	p := make(CacheItemPairList, len(table.items))
	i := 0
	for k, v := range table.items {
		p[i] = CacheItemPair{k, v.accessCount}
		i++
	}
	// 排序
	sort.Sort(p)
	// 用于返回
	var items []*CacheItem
	// 数据转换
	c := int64(0)
	for _, val := range p {
		if c >= count {
			break
		}
		item, ok := table.items[val.Key]
		if ok {
			items = append(items, item)
		}
		c++
	}
	return items
}

// 设置日志对象
func (table *CacheTable) SetLogger(logger *log.Logger) {
	table.RLock()
	defer table.RUnlock()
	table.logger = logger
}

func (table *CacheTable) log(v ...interface{}) {
	if table.logger == nil {
		return
	}
	table.logger.Println(v...)
}
