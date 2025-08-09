package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alt-coder/go-mean-reversion/pkg/broker/dhan"
	"github.com/alt-coder/go-mean-reversion/pkg/models"
	"github.com/gorilla/mux"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// DhanOrderWebhook represents the incoming webhook payload from Dhan
// Alias to shared model
type DhanOrderWebhook = models.DhanOrderWebhook

// WebhookServer holds the configuration for the webhook server
type WebhookServer struct {
	sheetsService  *sheets.Service
	spreadsheetID  string
	orderBookRange string
}

// Constants
const (
	defaultSpreadsheetID  = "1KyAGZwzW7spZeBf6Tmz-xKzNLLwOBruNMDsAzMvpwaA"
	defaultOrderBookRange = "Order_Book!A2:F"
	serverPort            = ":8080"
)

func main() {
	log.Println("Starting Dhan Webhook Server...")

	// Initialize Google Sheets service
	sheetsService, err := createSheetsService()
	if err != nil {
		log.Fatalf("Failed to create Sheets service: %v", err)
	}

	// Create webhook server
	server := &WebhookServer{
		sheetsService:  sheetsService,
		spreadsheetID:  getEnvWithDefault("SPREADSHEET_ID", defaultSpreadsheetID),
		orderBookRange: getEnvWithDefault("ORDER_BOOK_RANGE", defaultOrderBookRange),
	}

	// Setup routes
	router := mux.NewRouter()
	router.HandleFunc("/webhook/dhan/order", server.handleDhanOrderWebhook).Methods("POST")
	router.HandleFunc("/health", server.handleHealth).Methods("GET")

	// Start server
	log.Printf("Webhook server listening on port %s", serverPort)
	log.Printf("Webhook endpoint: http://localhost%s/webhook/dhan/order", serverPort)
	dhan.SetupSecurityCache(os.Getenv("CSV_PATH"), nil)
	log.Fatal(http.ListenAndServe(serverPort, router))
}

// handleDhanOrderWebhook processes incoming Dhan order webhooks
func (ws *WebhookServer) handleDhanOrderWebhook(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received webhook request from %s", r.RemoteAddr)

	// Parse JSON payload
	var webhook DhanOrderWebhook
	if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
		log.Printf("Error parsing webhook payload: %v", err)
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	log.Printf("Processing order webhook: OrderID=%s, Symbol=%s, Type=%s, Status=%s",
		webhook.OrderID, webhook.TradingSymbol, webhook.TransactionType, webhook.OrderStatus)

	// Only process completed orders (TRADED status)
	if webhook.OrderStatus != "TRADED" {
		log.Printf("Ignoring order with status: %s", webhook.OrderStatus)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ignored",
			"reason": fmt.Sprintf("Order status is %s, not TRADED", webhook.OrderStatus),
		})
		return
	}

	// Get trading symbol from security ID if trading symbol is empty
	tradingSymbol := webhook.TradingSymbol
	if tradingSymbol == "" {
		symbol, err := dhan.GetSymbolByID(webhook.SecurityID)
		if err != nil {
			log.Printf("Warning: Could not get trading symbol for security ID %s: %v", webhook.SecurityID, err)
			tradingSymbol = fmt.Sprintf("SEC_%s", webhook.SecurityID)
		} else {
			tradingSymbol = symbol
		}
	}

	// Update Google Sheets
	if err := ws.updateOrderBook(webhook, tradingSymbol); err != nil {
		log.Printf("Error updating order book: %v", err)
		http.Error(w, "Failed to update order book", http.StatusInternalServerError)
		return
	}

	// Send success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Order book updated successfully",
		"orderId": webhook.OrderID,
	})

	log.Printf("Successfully processed webhook for order %s", webhook.OrderID)
}

// handleHealth provides a health check endpoint
func (ws *WebhookServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// updateOrderBook adds a new entry to the Google Sheets order book
func (ws *WebhookServer) updateOrderBook(webhook DhanOrderWebhook, tradingSymbol string) error {
	// Parse the create time or use current time
	var orderDate string
	if webhook.CreateTime != "" {
		// Parse the timestamp format: "2021-11-24 13:33:03"
		if t, err := time.Parse("2006-01-02 15:04:05", webhook.CreateTime); err == nil {
			orderDate = t.Format("2006-01-02")
		} else {
			log.Printf("Warning: Could not parse create time %s, using current date", webhook.CreateTime)
			orderDate = time.Now().Format("2006-01-02")
		}
	} else {
		orderDate = time.Now().Format("2006-01-02")
	}

	// Prepare the row data: Date, Stock Symbol, Action, Quantity, Price
	row := []interface{}{
		orderDate,
		tradingSymbol,
		strings.ToUpper(webhook.TransactionType), // BUY or SELL
		webhook.Quantity,
		webhook.Price,
	}

	// Create the value range
	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{row},
	}

	// Append to the sheet
	_, err := ws.sheetsService.Spreadsheets.Values.Append(
		ws.spreadsheetID,
		ws.orderBookRange,
		valueRange,
	).ValueInputOption("RAW").Do()

	if err != nil {
		return fmt.Errorf("failed to append to sheet: %v", err)
	}

	log.Printf("Added order to sheet: %s %s %s %d @ %.2f",
		orderDate, tradingSymbol, webhook.TransactionType, webhook.Quantity, webhook.Price)

	return nil
}

// createSheetsService creates Google Sheets service from environment variables
func createSheetsService() (*sheets.Service, error) {
	ctx := context.Background()

	// Get credentials from environment variables
	googleClientEmail := os.Getenv("GOOGLE_CLIENT_EMAIL")
	googlePrivateKey := os.Getenv("GOOGLE_PRIVATE_KEY")
	googleProjectID := os.Getenv("GOOGLE_PROJECT_ID")

	// Try environment variables first
	if googleClientEmail != "" && googlePrivateKey != "" {
		return createSheetsServiceFromEnv(ctx, googleClientEmail, googlePrivateKey, googleProjectID)
	}

	// Fallback to default credentials
	return sheets.NewService(ctx, option.WithScopes(sheets.SpreadsheetsScope))
}

// createSheetsServiceFromEnv creates service using env variables
func createSheetsServiceFromEnv(ctx context.Context, clientEmail, privateKey, projectID string) (*sheets.Service, error) {
	// Parse the private key
	privateKeyData := strings.ReplaceAll(privateKey, "\\n", "\n")

	// Create JWT config with raw private key bytes
	config := &jwt.Config{
		Email:      clientEmail,
		PrivateKey: []byte(privateKeyData),
		Scopes:     []string{sheets.SpreadsheetsScope},
		TokenURL:   google.JWTTokenURL,
	}

	if projectID != "" {
		config.Subject = projectID
	}

	// Create HTTP client
	client := config.Client(ctx)

	// Create Sheets service
	return sheets.NewService(ctx, option.WithHTTPClient(client))
}

// getEnvWithDefault returns environment variable value or default if not set
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
