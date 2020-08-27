package cache

import (
	"testing"
	"time"
)

var (
	k = "testkey"
	v = "testvalue"
)

func TestCache(t *testing.T) {
	// add an expiring item after a non-expiring one to
	// trigger expirationCheck iterating over non-expiring items
	table := Cache("testCacheaaa")
	table.Add(k+"_1", 0*time.Second, v)
	table.Add(k+"_2", 1*time.Second, v)

	// check if both items are still there
	p, err := table.Value(k + "_1")
	if err != nil || p == nil || p.Value().(string) != v {
		t.Error("Error retrieving non expiring data from cache", err)
	}
	p, err = table.Value(k + "_2")
	if err != nil || p == nil || p.Value().(string) != v {
		t.Error("Error retrieving data from cache", err)
	}

	// sanity checks
	if p.AccessCount() != 1 {
		t.Error("Error getting correct access count")
	}
	if p.LifeSpan() != 1*time.Second {
		t.Error("Error getting correct life-span")
	}
	if p.AccessedOn().Unix() == 0 {
		t.Error("Error getting access time")
	}
	if p.CreatedOn().Unix() == 0 {
		t.Error("Error getting creation time")
	}
}

func TestExists(t *testing.T) {
	// add an expiring item
	table := Cache("testExists")
	table.Add(k, 0, v)
	// check if it exists
	if !table.Exists(k) {
		t.Error("Error verifying existing data in cache")
	}
}