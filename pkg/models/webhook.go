package models

// DhanOrderWebhook represents the incoming webhook payload from Dhan.
type DhanOrderWebhook struct {
	DhanClientID        string      `json:"dhanClientId"`
	OrderID             string      `json:"orderId"`
	CorrelationID       string      `json:"correlationId"`
	OrderStatus         string      `json:"orderStatus"`
	TransactionType     string      `json:"transactionType"`
	ExchangeSegment     string      `json:"exchangeSegment"`
	ProductType         string      `json:"productType"`
	OrderType           string      `json:"orderType"`
	Validity            string      `json:"validity"`
	TradingSymbol       string      `json:"tradingSymbol"`
	SecurityID          string      `json:"securityId"`
	Quantity            int         `json:"quantity"`
	DisclosedQuantity   int         `json:"disclosedQuantity"`
	Price               float64     `json:"price"`
	TriggerPrice        float64     `json:"triggerPrice"`
	AfterMarketOrder    bool        `json:"afterMarketOrder"`
	BoProfitValue       float64     `json:"boProfitValue"`
	BoStopLossValue     float64     `json:"boStopLossValue"`
	LegName             interface{} `json:"legName"`
	CreateTime          string      `json:"createTime"`
	UpdateTime          string      `json:"updateTime"`
	ExchangeTime        string      `json:"exchangeTime"`
	DrvExpiryDate       interface{} `json:"drvExpiryDate"`
	DrvOptionType       interface{} `json:"drvOptionType"`
	DrvStrikePrice      float64     `json:"drvStrikePrice"`
	OmsErrorCode        interface{} `json:"omsErrorCode"`
	OmsErrorDescription interface{} `json:"omsErrorDescription"`
	BoLegDetails        interface{} `json:"boLegDetails"`
	Message             string      `json:"message"`
}
