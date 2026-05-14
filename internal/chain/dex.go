package chain

import (
	"errors"
	"sync"
)

// Pool represents a liquidity pool for a pair of assets.
// One asset is always the native SYN coin.
type Pool struct {
	AssetID   string  `json:"asset_id"` // ID of the non-SYN asset
	SynReserve uint64 `json:"syn_reserve"`
	AssetReserve uint64 `json:"asset_reserve"`
	TotalShares uint64 `json:"total_shares"`
	Shares      map[Address]uint64 `json:"shares"`
}

type DEX struct {
	mu    sync.RWMutex
	Pools map[string]*Pool // AssetID -> Pool
}

func NewDEX() *DEX {
	return &DEX{
		Pools: make(map[string]*Pool),
	}
}

// GetAmountOut calculates the output amount for a swap using x * y = k
func (d *DEX) GetAmountOut(amountIn uint64, reserveIn uint64, reserveOut uint64) (uint64, error) {
	if amountIn == 0 {
		return 0, errors.New("insufficient input amount")
	}
	if reserveIn == 0 || reserveOut == 0 {
		return 0, errors.New("insufficient liquidity")
	}

	// 0.3% fee
	amountInWithFee := amountIn * 997
	numerator := amountInWithFee * reserveOut
	denominator := (reserveIn * 1000) + amountInWithFee
	
	return numerator / denominator, nil
}

// AddLiquidity adds liquidity to a pool
func (d *DEX) AddLiquidity(assetID string, synAmount uint64, assetAmount uint64, provider Address) (uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	pool, exists := d.Pools[assetID]
	if !exists {
		pool = &Pool{
			AssetID: assetID,
			Shares:  make(map[Address]uint64),
		}
		d.Pools[assetID] = pool
	}

	var shares uint64
	if pool.TotalShares == 0 {
		shares = synAmount 
	} else {
		shareSyn := (synAmount * pool.TotalShares) / pool.SynReserve
		shareAsset := (assetAmount * pool.TotalShares) / pool.AssetReserve
		if shareSyn < shareAsset {
			shares = shareSyn
		} else {
			shares = shareAsset
		}
	}

	if shares == 0 {
		return 0, errors.New("insufficient liquidity minted")
	}

	pool.SynReserve += synAmount
	pool.AssetReserve += assetAmount
	pool.TotalShares += shares
	pool.Shares[provider] += shares

	return shares, nil
}

// Swap swaps tokens
func (d *DEX) Swap(assetID string, amountIn uint64, fromSyn bool) (uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	pool, exists := d.Pools[assetID]
	if !exists {
		return 0, errors.New("pool not found")
	}

	var amountOut uint64
	var err error

	if fromSyn {
		amountOut, err = d.GetAmountOut(amountIn, pool.SynReserve, pool.AssetReserve)
		if err == nil {
			pool.SynReserve += amountIn
			pool.AssetReserve -= amountOut
		}
	} else {
		amountOut, err = d.GetAmountOut(amountIn, pool.AssetReserve, pool.SynReserve)
		if err == nil {
			pool.AssetReserve += amountIn
			pool.SynReserve -= amountOut
		}
	}

	return amountOut, err
}

// SeedPool manually initializes a pool with liquidity (for genesis/testing)
func (d *DEX) SeedPool(assetID string, synAmount uint64, assetAmount uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Pools[assetID] = &Pool{
		AssetID:      assetID,
		SynReserve:   synAmount,
		AssetReserve: assetAmount,
		TotalShares:  synAmount,
		Shares:       make(map[Address]uint64),
	}
}

func (d *DEX) ListPools() map[string]*Pool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Pools
}
