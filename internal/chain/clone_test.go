package chain

import (
	"reflect"
	"testing"
)

// TestStateClone_CopiesEveryExportedField guards against the exact bug the
// audit found: State.Clone() was written before CitizenStakes,
// TreasuryAddress, and CitizenRewardRateBpsPerYear existed and was never
// updated to copy them. Because FinalizeBlock commits every block as
// c.State.Clone() + applied txs, then replaces c.State with that clone,
// those three fields were silently reset to zero on every single block --
// wiping Citizen stakes (after the staker's balance had already been
// debited -- a real, permanent loss of funds, not a display bug) and
// resetting TreasuryAddress/CitizenRewardRateBpsPerYear within one block of
// genesis.
//
// Rather than just asserting the three fields this audit found, this test
// walks every exported field of State via reflection and fails if ANY of
// them isn't copied by Clone -- so the next field someone adds to State
// can't repeat this bug by being forgotten in Clone the same way.
func TestStateClone_CopiesEveryExportedField(t *testing.T) {
	addr1 := Address("0x1111111111111111111111111111111111111111")
	addr2 := Address("0x2222222222222222222222222222222222222222")

	s := NewState()
	s.Accounts[addr1] = Account{Balance: 500, Nonce: 3, Assets: map[string]uint64{"NGOT": 7}}
	s.ImmuneNodes[addr1] = ImmuneNodeRecord{}
	s.SovereignProofs["proof-1"] = SovereignProofRecord{}
	s.BridgeEvents["event-1"] = BridgeRecord{ID: "event-1"}
	s.ProcessedBridgeEvents["event-1"] = true
	s.BridgeValidators["validator-1"] = "0xdeadbeef"
	s.BridgeQuorum = 2
	s.LastSovereignProofID = "proof-1"
	s.LastBridgeEventID = "event-1"
	s.TotalSupply = 123456
	s.CitizenStakes[addr2] = CitizenStake{Amount: 999, StakedAt: 1000, LastClaimAt: 1000}
	s.TreasuryAddress = addr1
	s.CitizenRewardRateBpsPerYear = 250

	clone := s.Clone()

	sVal := reflect.ValueOf(s).Elem()
	cloneVal := reflect.ValueOf(clone).Elem()
	typ := sVal.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue // e.g. the mutex -- not state to copy, deliberately skipped
		}
		got := cloneVal.Field(i).Interface()
		want := sVal.Field(i).Interface()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Clone did not copy field %q: source=%#v clone=%#v", field.Name, want, got)
		}
	}
}
