# Automated Trading Bot

An intelligent trading bot that analyzes Nifty 50 stocks and executes trades based on technical indicators. The bot integrates with Dhan broker API, sends notifications via Telegram, and logs all activities to Google Sheets.

## Features

- **Automated Stock Analysis**: Analyzes Nifty 50 stocks using 20-day Simple Moving Average (SMA)
- **Smart Position Management**: Implements both new position entry and averaging down strategies
- **Telegram Integration**: Sends trade proposals and requires manual approval before execution
- **Google Sheets Logging**: Maintains detailed order book in Google Sheets
- **Scheduled Execution**: Runs automatically at 15:20 IST on trading days
- **After Market Orders**: Supports AMO (After Market Orders) when markets are closed
- **Holiday Detection**: Skips execution on weekends and predefined holidays

## How It Works

### Strategy Logic

1. **Stock Selection**: Fetches Nifty 50 symbols from Google Sheets
2. **Technical Analysis**: Calculates 20-day SMA deviation for each stock
3. **Ranking**: Selects top 5 stocks with highest negative deviation (oversold)
4. **Position Management**:
   - **Entry Mode**: Opens new positions when not all top 5 stocks are held
   - **Averaging Mode**: Averages down on worst performing held stock when threshold is breached

### Execution Flow

1. Daily execution at 15:20 IST (Monday-Friday)
2. Analysis and action determination
3. Telegram notification with proposed trades
4. Manual approval required via Telegram button
5. Order execution via Dhan API
6. Results logged to Google Sheets

## Prerequisites

- Go 1.21 or higher
- Docker (optional)
- Dhan trading account with API access
- Telegram bot token
- Google Cloud Service Account with Sheets API access

## Installation

### Option 1: Docker (Recommended)

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd trading-bot
   ```

2. **Set up environment variables**
   ```bash
   cp .env.example .env
   # Edit .env with your actual values
   ```

3. **Add Google Service Account credentials**
   - Place your service account JSON file as `credentials.json` in the project root

4. **Run with Docker Compose**
   ```bash
   docker-compose up -d
   ```

### Option 2: Local Development

1. **Install dependencies**
   ```bash
   go mod tidy
   ```

2. **Set environment variables**
   ```bash
   export DHAN_CLIENT_ID="your_client_id"
   export DHAN_ACCESS_TOKEN="your_access_token"
   export TELEGRAM_BOT_TOKEN="your_bot_token"
   export TELEGRAM_CHAT_ID="your_chat_id"
   export MAX_NEW_POSITIONS="3"
   export AVERAGING_THRESHOLD="-0.05"
   ```

3. **Run the application**
   ```bash
   go run trading_bot.go
   ```

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `DHAN_CLIENT_ID` | Your Dhan client ID | - | Yes |
| `DHAN_ACCESS_TOKEN` | Your Dhan access token | - | Yes |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token | - | Yes |
| `TELEGRAM_CHAT_ID` | Your Telegram chat ID | - | Yes |
| `MAX_NEW_POSITIONS` | Maximum new positions per day | 3 | No |
| `AVERAGING_THRESHOLD` | Threshold for averaging down (negative value) | -0.05 | No |

### Google Sheets Setup

1. **Create a Google Sheet** with the following structure:

   **Sheet 1: "Nifty50_Data"**
   - Column A: Stock symbols (e.g., RELIANCE, TCS, INFY)
   - Start from row 2

   **Sheet 2: "Order_Book"**
   - Column A: Timestamp
   - Column B: Symbol
   - Column C: Action Type
   - Column D: Quantity
   - Column E: Price
   - Column F: Status

2. **Update the spreadsheet ID** in the code:
   ```go
   const spreadsheetID = "your_google_sheet_id"
   ```

### Telegram Bot Setup

1. **Create a bot** via [@BotFather](https://t.me/botfather)
2. **Get your chat ID** by messaging [@userinfobot](https://t.me/userinfobot)
3. **Start a conversation** with your bot before running the application

## Usage

### Manual Testing

You can test the bot manually by triggering the strategy:

```bash
# If running locally
curl -X POST http://localhost:8080/trigger

# Or modify the cron schedule for testing
```

### Monitoring

- **Logs**: Check Docker logs for execution details
  ```bash
  docker-compose logs -f trading-bot
  ```

- **Telegram**: Receive real-time notifications and approve trades
- **Google Sheets**: Monitor all executed trades and their status

## Security Considerations

- **API Keys**: Never commit API keys to version control
- **Credentials**: Store Google Service Account JSON securely
- **Network**: Consider running in a private network
- **Monitoring**: Set up alerts for failed executions

## Troubleshooting

### Common Issues

1. **Authentication Errors**
   - Verify Dhan API credentials
   - Check Google Service Account permissions
   - Ensure Telegram bot token is correct

2. **Market Data Issues**
   - Yahoo Finance API might be rate-limited
   - Check internet connectivity
   - Verify stock symbols format

3. **Scheduling Issues**
   - Confirm timezone settings (IST)
   - Check if running on trading days
   - Verify cron expression

### Debug Mode

Enable verbose logging by modifying the log level:

```go
log.SetLevel(log.DebugLevel)
```

## Risk Disclaimer

⚠️ **Important**: This trading bot is for educational purposes. Trading involves significant financial risk. Always:

- Test thoroughly with paper trading first
- Start with small position sizes
- Monitor all trades closely
- Understand the strategy completely
- Never risk more than you can afford to lose

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues and questions:
- Create an issue in the repository
- Check the troubleshooting section
- Review logs for error details

---

**Disclaimer**: This software is provided as-is without any warranty. Use at your own risk.