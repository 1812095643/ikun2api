package adminapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// The page polls every 10 seconds, possibly from several tabs; one computation
// per pageCacheTTL serves them all. Writes clear the cache.
const pageCacheTTL = 5 * time.Second

type cachedPage struct {
	at     time.Time
	status int
	body   []byte
}

type pageCache struct {
	mu    sync.Mutex
	pages map[string]cachedPage
	locks map[string]*sync.Mutex
}

func (c *pageCache) clear() {
	c.mu.Lock()
	c.pages = nil
	c.mu.Unlock()
}

func (c *pageCache) lock(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.locks == nil {
		c.locks = map[string]*sync.Mutex{}
	}
	if c.locks[key] == nil {
		c.locks[key] = &sync.Mutex{}
	}
	return c.locks[key]
}

func (c *pageCache) get(key string, now time.Time) (cachedPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pages[key]
	return p, ok && now.Sub(p.at) < pageCacheTTL
}

func (c *pageCache) put(key string, p cachedPage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pages == nil {
		c.pages = map[string]cachedPage{}
	}
	c.pages[key] = p
}

func (a *API) cached(key string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serve := func(p cachedPage) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(p.status)
			w.Write(p.body)
		}
		if p, ok := a.cache.get(key, time.Now()); ok {
			serve(p)
			return
		}
		// Concurrent callers wait for the one computation instead of repeating it.
		l := a.cache.lock(key)
		l.Lock()
		defer l.Unlock()
		if p, ok := a.cache.get(key, time.Now()); ok {
			serve(p)
			return
		}
		rec := httptest.NewRecorder()
		h(rec, r)
		p := cachedPage{at: time.Now(), status: rec.Code, body: bytes.Clone(rec.Body.Bytes())}
		if p.status == http.StatusOK {
			a.cache.put(key, p)
		}
		serve(p)
	}
}
