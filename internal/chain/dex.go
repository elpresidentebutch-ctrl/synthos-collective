package chain

import (
	"errors"
	"math/bits"
	"strings"
	"sync"
)

// Pool represents a liquidity pool for a pair of assets.
// One asset is always the native SYN coin.
type Pool struct {
	AssetID      string             `json:"asset_id"` // ID of the non-SYN asset
	SynReserve   uint64             `json:"syn_reserve"`
	AssetReserve uint64             `json:"asset_reserve"`
	TotalShares  uint64             `json:"total_shares"`
	Shares       map[Address]uint64 `json:"shares"`
}

// PoolAddress returns the deterministic on-chain account address for an asset pair's reserves.
func PoolAddress(assetID string) Address {
	clean := strings.ToLower(strings.TrimSpace(assetID))
	return Address("0xdex_pool_" + clean)
}

func clonePool(p *Pool) *Pool {
	if p == nil {
		return nil
	}
	cp := &Pool{
		AssetID:      p.AssetID,
		SynReserve:   p.SynReserve,
		AssetReserve: p.AssetReserve,
		TotalShares:  p.TotalShares,
		Shares:       make(map[Address]uint64, len(p.Shares)),
	}
	for address, shares := range p.Shares {
		cp.Shares[address] = shares
	}
	return cp
}

func sqrtUint64(x uint64) uint64 {
	if x == 0 {
		return 0
	}
	z := (x + 1) / 2
	y := x
	for z < y {
		y = z
		z = (x/z + z) / 2
	}
	return y
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

// GetAmountOut calculates the output amount for a swap using x * y = k with a 0.3% (30 bps) fee.
func (d *DEX) GetAmountOut(amountIn uint64, reserveIn uint64, reserveOut uint64) (uint64, error) {
	if amountIn == 0 {
		return 0, errors.New("insufficient input amount")
	}
	if reserveIn == 0 || reserveOut == 0 {
		return 0, errors.New("insufficient liquidity")
	}

	// 0.3% fee (99.7% of input amount contributes to trade)
	amountInHi, amountInWithFee := bits.Mul64(amountIn, 997)
	if amountInHi != 0 {
		return 0, errors.New("swap input overflow")
	}
	numeratorHi, numerator := bits.Mul64(amountInWithFee, reserveOut)
	if numeratorHi != 0 {
		return 0, errors.New("swap numerator overflow")
	}
	reserveHi, scaledReserve := bits.Mul64(reserveIn, 1000)
	if reserveHi != 0 {
		return 0, errors.New("swap reserve overflow")
	}
	denominator, carry := bits.Add64(scaledReserve, amountInWithFee, 0)
	if carry != 0 || denominator == 0 {
		return 0, errors.New("swap denominator overflow")
	}

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
		shares = sqrtUint64(synAmount * assetAmount)
		if shares == 0 {
			shares = synAmount
		}
	} else {
		if pool.SynReserve == 0 || pool.AssetReserve == 0 {
			return 0, errors.New("pool has shares but a zero reserve")
		}
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

// RemoveLiquidity burns provider shares and returns proportionate SYN and asset reserves.
func (d *DEX) RemoveLiquidity(assetID string, shares uint64, provider Address) (uint64, uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	pool, exists := d.Pools[assetID]
	if !exists {
		return 0, 0, errors.New("pool not found")
	}
	if shares == 0 {
		return 0, 0, errors.New("invalid shares")
	}
	if pool.TotalShares == 0 || pool.SynReserve == 0 || pool.AssetReserve == 0 {
		return 0, 0, errors.New("insufficient pool liquidity")
	}
	providerShares := pool.Shares[provider]
	if providerShares < shares {
		return 0, 0, errors.New("insufficient provider shares")
	}

	synOut := (shares * pool.SynReserve) / pool.TotalShares
	assetOut := (shares * pool.AssetReserve) / pool.TotalShares
	if synOut == 0 || assetOut == 0 {
		return 0, 0, errors.New("zero output amount")
	}

	pool.Shares[provider] -= shares
	if pool.Shares[provider] == 0 {
		delete(pool.Shares, provider)
	}
	pool.TotalShares -= shares
	pool.SynReserve -= synOut
	pool.AssetReserve -= assetOut

	return synOut, assetOut, nil
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
			nextReserve, err := safeAdd(pool.SynReserve, amountIn)
			if err != nil {
				return 0, errors.New("SYN reserve overflow")
			}
			pool.SynReserve = nextReserve
			pool.AssetReserve -= amountOut
		}
	} else {
		amountOut, err = d.GetAmountOut(amountIn, pool.AssetReserve, pool.SynReserve)
		if err == nil {
			nextReserve, err := safeAdd(pool.AssetReserve, amountIn)
			if err != nil {
				return 0, errors.New("asset reserve overflow")
			}
			pool.AssetReserve = nextReserve
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

func (d *DEX) SyncFromState(state *State) {
	if state == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Pools = state.ListDEXPools()
}

func (d *DEX) ListPools() map[string]*Pool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make(map[string]*Pool, len(d.Pools))
	for assetID, pool := range d.Pools {
		if pool == nil {
			continue
		}
		copyPool := *pool
		copyPool.Shares = make(map[Address]uint64, len(pool.Shares))
		for address, shares := range pool.Shares {
			copyPool.Shares[address] = shares
		}
		out[assetID] = &copyPool
	}
	return out
}
