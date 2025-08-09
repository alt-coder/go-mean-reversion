package cache

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// Cache stores a limited set of trading symbol to security ID mappings
// loaded from a CSV file on demand. Entries expire after the TTL and
// are reloaded when requested again.
type Cache struct {
	path   string
	ttl    time.Duration
	filter map[string]struct{}

	mu      sync.Mutex
	symToID map[string]string
	idToSym map[string]string
	last    time.Time
}

// New creates a new security cache. If symbols is non-empty, only those
// symbols will be considered when loading from the CSV.
func New(path string, symbols []string, ttl time.Duration) *Cache {
	f := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		f[s] = struct{}{}
	}
	return &Cache{path: path, ttl: ttl, filter: f, symToID: make(map[string]string), idToSym: make(map[string]string)}
}

// GetID returns the security ID for the given trading symbol. The CSV is
// scanned if the symbol is not cached or the cache has expired.
func (c *Cache) GetID(symbol string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.filter[symbol]; !ok {
		c.filter[symbol] = struct{}{}
	}
	if c.expired() || c.symToID[symbol] == "" {
		if err := c.refresh(); err != nil {
			return "", err
		}
	}
	if id, ok := c.symToID[symbol]; ok {
		return id, nil
	}
	return "", fmt.Errorf("symbol %q not found in CSV", symbol)
}

// GetSymbol returns the trading symbol for the given security ID.
func (c *Cache) GetSymbol(securityID string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.expired() || c.idToSym[securityID] == "" {
		if err := c.refresh(); err != nil {
			return "", err
		}
	}
	if sym, ok := c.idToSym[securityID]; ok {
		return sym, nil
	}
	// fall back to direct lookup and expand filter if needed
	sym, err := c.lookupID(securityID)
	if err != nil {
		return "", err
	}
	c.idToSym[securityID] = sym
	c.symToID[sym] = securityID
	c.filter[sym] = struct{}{}
	return sym, nil
}

func (c *Cache) expired() bool {
	return !c.last.IsZero() && time.Since(c.last) > c.ttl
}

// refresh reloads the cache for all tracked symbols.
func (c *Cache) refresh() error {
	f, err := os.Open(c.path)
	if err != nil {
		return fmt.Errorf("opening CSV: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	symToID := make(map[string]string)
	idToSym := make(map[string]string)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading CSV: %w", err)
		}
		if len(row) < 6 {
			log.Printf("Skipping row due to unexpected format: %v", row)
			continue
		}
		sym := row[5]
		if len(c.filter) > 0 {
			if _, ok := c.filter[sym]; !ok {
				continue
			}
		}
		id := row[2]
		symToID[sym] = id
		idToSym[id] = sym
	}
	c.symToID = symToID
	c.idToSym = idToSym
	c.last = time.Now()
	return nil
}

// lookupID scans the CSV for the provided security ID and updates the filter.
func (c *Cache) lookupID(id string) (string, error) {
	f, err := os.Open(c.path)
	if err != nil {
		return "", fmt.Errorf("opening CSV: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading CSV: %w", err)
		}
		if len(row) < 6 {
			log.Printf("Skipping row due to unexpected format: %v", row)
			continue
		}
		if row[2] == id {
			sym := row[5]
			c.filter[sym] = struct{}{}
			return sym, nil
		}
	}
	return "", fmt.Errorf("security id %q not found in CSV", id)
}
