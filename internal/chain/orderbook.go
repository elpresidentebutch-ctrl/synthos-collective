package chain

import (
	"sort"
	"sync"
	"time"
)

type Side int

const (
	Buy Side = iota
	Sell
)

type Order struct {
	ID        string
	Owner     Address
	AssetPair string
	Side      Side
	Price     uint64
	Amount    uint64
	Filled    uint64
	Timestamp int64
}

type OrderBook struct {
	AssetPair string
	Bids      []*Order
	Asks      []*Order
	mu        sync.RWMutex
}

func NewOrderBook(pair string) *OrderBook {
	return &OrderBook{
		AssetPair: pair,
		Bids:      make([]*Order, 0),
		Asks:      make([]*Order, 0),
	}
}

// PlaceOrder adds an order to the book and attempts to match it immediately (Market/Limit behavior)
func (ob *OrderBook) PlaceOrder(order *Order) []*Trade {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	order.Timestamp = time.Now().UnixNano()
	var trades []*Trade

	if order.Side == Buy {
		trades = ob.matchBuy(order)
		if order.Filled < order.Amount {
			ob.Bids = append(ob.Bids, order)
			sort.Slice(ob.Bids, func(i, j int) bool {
				if ob.Bids[i].Price == ob.Bids[j].Price {
					return ob.Bids[i].Timestamp < ob.Bids[j].Timestamp
				}
				return ob.Bids[i].Price > ob.Bids[j].Price // Highest bid first
			})
		}
	} else {
		trades = ob.matchSell(order)
		if order.Filled < order.Amount {
			ob.Asks = append(ob.Asks, order)
			sort.Slice(ob.Asks, func(i, j int) bool {
				if ob.Asks[i].Price == ob.Asks[j].Price {
					return ob.Asks[i].Timestamp < ob.Asks[j].Timestamp
				}
				return ob.Asks[i].Price < ob.Asks[j].Price // Lowest ask first
			})
		}
	}

	return trades
}

func (ob *OrderBook) matchBuy(order *Order) []*Trade {
	var trades []*Trade
	for i := 0; i < len(ob.Asks) && order.Filled < order.Amount; {
		ask := ob.Asks[i]
		if order.Price < ask.Price {
			break // No more matches possible for Limit Buy
		}

		fillAmount := min(order.Amount-order.Filled, ask.Amount-ask.Filled)
		order.Filled += fillAmount
		ask.Filled += fillAmount

		trades = append(trades, &Trade{
			MakerID: ask.ID,
			TakerID: order.ID,
			Price:   ask.Price,
			Amount:  fillAmount,
		})

		if ask.Filled == ask.Amount {
			ob.Asks = append(ob.Asks[:i], ob.Asks[i+1:]...)
		} else {
			i++
		}
	}
	return trades
}

func (ob *OrderBook) matchSell(order *Order) []*Trade {
	var trades []*Trade
	for i := 0; i < len(ob.Bids) && order.Filled < order.Amount; {
		bid := ob.Bids[i]
		if order.Price > bid.Price {
			break // No more matches possible for Limit Sell
		}

		fillAmount := min(order.Amount-order.Filled, bid.Amount-bid.Filled)
		order.Filled += fillAmount
		bid.Filled += fillAmount

		trades = append(trades, &Trade{
			MakerID: bid.ID,
			TakerID: order.ID,
			Price:   bid.Price,
			Amount:  fillAmount,
		})

		if bid.Filled == bid.Amount {
			ob.Bids = append(ob.Bids[:i], ob.Bids[i+1:]...)
		} else {
			i++
		}
	}
	return trades
}

type Trade struct {
	MakerID string
	TakerID string
	Price   uint64
	Amount  uint64
}

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
