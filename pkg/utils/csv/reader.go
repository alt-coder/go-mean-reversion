package csv

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
)

// LoadCache reads a CSV file and returns a map of trading symbols to security IDs.
// The symbols map is used to filter results; if empty, all symbols are returned.
func LoadCache(path string, symbols map[string]struct{}) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening CSV: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading CSV: %w", err)
	}

	cache := make(map[string]string, len(records)-1)
	for i, row := range records {
		if i == 0 {
			// Skip header row
			continue
		}
		if len(row) < 6 {
			log.Printf("Skipping row %d due to unexpected format: %v", i, row)
			continue
		}
		tradingSymbol := row[5]
		if len(symbols) > 0 {
			if _, ok := symbols[tradingSymbol]; !ok {
				continue
			}
		}
		securityID := row[2]
		cache[tradingSymbol] = securityID
	}

	return cache, nil
}
