package dhan

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

var (
	csvPath string
	filter  map[string]struct{}
	symToID map[string]string
	idToSym map[string]string
	last    time.Time
	mu      sync.Mutex
)

// SetupSecurityCache configures the CSV path and optional symbol filter for security ID lookups.
// The symbols slice can be nil or empty to include all entries from the CSV.
func SetupSecurityCache(path string, symbols []string) {
	mu.Lock()
	defer mu.Unlock()
	csvPath = path
	filter = make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		filter[s] = struct{}{}
	}
	symToID = nil
	idToSym = nil
	last = time.Time{}
}

// GetSecurityID returns the security ID for the given trading symbol.
func GetSecurityID(symbol string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if err := ensureCache(); err != nil {
		return "", err
	}
	id, ok := symToID[symbol]
	if !ok {
		return "", fmt.Errorf("symbol %q not found in cache", symbol)
	}
	return id, nil
}

// GetSymbolByID returns the trading symbol for the given security ID.
func GetSymbolByID(securityID string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if err := ensureCache(); err != nil {
		return "", err
	}
	sym, ok := idToSym[securityID]
	if !ok {
		return "", fmt.Errorf("security id %q not found", securityID)
	}
	return sym, nil
}

// ensureCache loads the CSV cache if stale or not loaded.
func ensureCache() error {
	if symToID != nil && time.Since(last) <= 5*time.Minute {
		return nil
	}
	if csvPath == "" {
		return fmt.Errorf("CSV path not configured")
	}
	f, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("opening CSV: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("reading CSV: %w", err)
	}

	cache := make(map[string]string, len(records)-1)
	reverse := make(map[string]string, len(records)-1)
	for i, row := range records {
		if i == 0 {
			continue
		}
		if len(row) < 6 {
			log.Printf("Skipping row %d due to unexpected format: %v", i, row)
			continue
		}
		sym := row[5]
		if len(filter) > 0 {
			if _, ok := filter[sym]; !ok {
				continue
			}
		}
		id := row[2]
		cache[sym] = id
		reverse[id] = sym
	}
	symToID = cache
	idToSym = reverse
	last = time.Now()
	return nil
}
