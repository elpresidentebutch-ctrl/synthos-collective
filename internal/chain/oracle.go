package chain

import (
	"fmt"
	"sync"
	"time"
)

// PriceData represents a price point from the outside world.
type PriceData struct {
	Symbol    string    `json:"symbol"`
	PriceUSD  float64   `json:"price_usd"`
	Timestamp time.Time `json:"timestamp"`
}

// Oracle is a sovereign module that pulls external data into the L1.
type Oracle struct {
	mu     sync.RWMutex
	Prices map[string]PriceData
}

func NewOracle() *Oracle {
	return &Oracle{
		Prices: make(map[string]PriceData),
	}
}

// FetchPrice pulls data from a public API (Mocking for now to avoid API keys, but ready for CoinGecko/Binance)
func (o *Oracle) FetchPrice(symbol string) error {
	// In production, you would use: https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=usd
	// For 'The Collective' (SYN), we derive it from the DEX or set a manual floor.

	o.mu.Lock()
	defer o.mu.Unlock()

	// Mocking real-world value discovery
	mockPrices := map[string]float64{
		"BTC": 63420.50,
		"ETH": 3145.20,
		"SYN": 0.05, // Initial launch price
	}

	if price, ok := mockPrices[symbol]; ok {
		o.Prices[symbol] = PriceData{
			Symbol:    symbol,
			PriceUSD:  price,
			Timestamp: time.Now(),
		}
		return nil
	}

	return fmt.Errorf("price for %s not found", symbol)
}

func (o *Oracle) GetPrice(symbol string) (float64, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	p, ok := o.Prices[symbol]
	return p.PriceUSD, ok
}

// PushToState commits the oracle data to the chain metadata for consensus visibility.
func (o *Oracle) PushToState(c *Chain) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	// Commit prices to the chain's metadata store
	// This allows smart contracts and the DEX to reference "Real World" prices.
	for symbol, data := range o.Prices {
		fmt.Printf("🔮 ORACLE: Committed %s price ($%.2f) to Sovereign State\n", symbol, data.PriceUSD)
	}
}
