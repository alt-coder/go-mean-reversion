package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"
)

// loadEnvFile reads key=value lines from the given file and sets them in the process environment.
func loadEnvFile(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}
		os.Setenv(key, value)
	}
	return scanner.Err()
}

// TestMain ensures the trading bot's main function executes with environment configuration.
func TestMain(t *testing.T) {
	if err := loadEnvFile(".env"); err != nil {
		t.Skip("missing .env file")
	}
	go main()
	time.Sleep(100 * time.Millisecond)
}
