package sheets

import (
        "context"
        "fmt"
        "strconv"
        "time"

        "github.com/alt-coder/go-mean-reversion/pkg/models"
        "github.com/alt-coder/go-mean-reversion/pkg/config"
        "golang.org/x/oauth2/google"
        "golang.org/x/oauth2/jwt"
        "google.golang.org/api/option"
        "google.golang.org/api/sheets/v4"
)

// Predefined ranges within the spreadsheet.
const (
        Nifty50Range    = "Nifty50_Data!A2:A"
        TopCandRange    = "Dashboard!E2:G6"
        PortfolioRange  = "Portfolio!A2:F"
        OrderLogRange   = "Order_Book!A2:F"
)

// Client defines operations for interacting with Google Sheets.
type Client interface {
        ReadRange(ctx context.Context, sheetRange string) ([][]interface{}, error)
        AppendRow(ctx context.Context, sheetRange string, values []interface{}) error
        GetNifty50Symbols(ctx context.Context) ([]string, error)
        GetTopCandidates(ctx context.Context) ([]models.Candidate, error)
        GetPortfolio(ctx context.Context) ([]models.PortfolioStock, error)
        LogOrder(ctx context.Context, order models.OrderLog) error
}

// Service implements the Client interface using the Google Sheets API.
type Service struct {
        srv *sheets.Service
        id  string
}

// NewService creates a Sheets client using service account credentials from cfg.
func NewService(ctx context.Context, cfg config.SheetsConfig) (*Service, error) {
        conf := &jwt.Config{
                Email:      cfg.ClientEmail,
                PrivateKey: []byte(cfg.PrivateKey),
                Scopes:     []string{sheets.SpreadsheetsScope},
                TokenURL:   google.JWTTokenURL,
        }
        srv, err := sheets.NewService(ctx, option.WithHTTPClient(conf.Client(ctx)))
        if err != nil {
                return nil, err
        }
        return &Service{srv: srv, id: cfg.SpreadsheetID}, nil
}

// ReadRange retrieves cell values for the given range.
func (s *Service) ReadRange(ctx context.Context, sheetRange string) ([][]interface{}, error) {
        resp, err := s.srv.Spreadsheets.Values.Get(s.id, sheetRange).Context(ctx).Do()
        if err != nil {
                return nil, err
        }
        return resp.Values, nil
}

// AppendRow appends a row to the specified range.
func (s *Service) AppendRow(ctx context.Context, sheetRange string, values []interface{}) error {
        r := &sheets.ValueRange{Values: [][]interface{}{values}}
        _, err := s.srv.Spreadsheets.Values.Append(s.id, sheetRange, r).ValueInputOption("RAW").Context(ctx).Do()
        return err
}

// GetNifty50Symbols loads the list of Nifty50 symbols from the sheet.
func (s *Service) GetNifty50Symbols(ctx context.Context) ([]string, error) {
        values, err := s.ReadRange(ctx, Nifty50Range)
        if err != nil {
                return nil, err
        }
        syms := make([]string, 0, len(values))
        for _, row := range values {
                if len(row) > 0 {
                        syms = append(syms, fmt.Sprint(row[0]))
                }
        }
        return syms, nil
}

// GetTopCandidates returns top candidate stocks from the dashboard sheet.
func (s *Service) GetTopCandidates(ctx context.Context) ([]models.Candidate, error) {
        values, err := s.ReadRange(ctx, TopCandRange)
        if err != nil {
                return nil, err
        }
        cands := make([]models.Candidate, 0, len(values))
        for _, row := range values {
                if len(row) < 3 {
                        continue
                }
                dev, _ := strconv.ParseFloat(fmt.Sprint(row[1]), 64)
                price, _ := strconv.ParseFloat(fmt.Sprint(row[2]), 64)
                cands = append(cands, models.Candidate{Symbol: fmt.Sprint(row[0]), Deviation: dev, Price: price, Source: "sheets"})
        }
        return cands, nil
}

// GetPortfolio reads current portfolio holdings from the sheet.
func (s *Service) GetPortfolio(ctx context.Context) ([]models.PortfolioStock, error) {
        values, err := s.ReadRange(ctx, PortfolioRange)
        if err != nil {
                return nil, err
        }
        out := make([]models.PortfolioStock, 0, len(values))
        for _, row := range values {
                if len(row) < 6 {
                        continue
                }
                qty, _ := strconv.Atoi(fmt.Sprint(row[1]))
                avg, _ := strconv.ParseFloat(fmt.Sprint(row[2]), 64)
                cur, _ := strconv.ParseFloat(fmt.Sprint(row[3]), 64)
                pl, _ := strconv.ParseFloat(fmt.Sprint(row[4]), 64)
                pct, _ := strconv.ParseFloat(fmt.Sprint(row[5]), 64)
                out = append(out, models.PortfolioStock{
                        Symbol:        fmt.Sprint(row[0]),
                        Quantity:      qty,
                        AvgPrice:      avg,
                        CurrentPrice:  cur,
                        ProfitLoss:    pl,
                        PercentChange: pct,
                })
        }
        return out, nil
}

// LogOrder appends an order execution record to the Order_Book sheet.
func (s *Service) LogOrder(ctx context.Context, order models.OrderLog) error {
        row := []interface{}{order.Timestamp.Format(time.RFC3339), order.Symbol, order.Action, order.Lots, order.Price, order.Status}
        return s.AppendRow(ctx, OrderLogRange, row)
}

