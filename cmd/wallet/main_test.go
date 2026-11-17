package main

import "testing"

func TestTransferFee(t *testing.T) {
	cases := []struct {
		amount, user, base, want uint64
		wantErr                  bool
	}{
		{amount: 5_000, want: 10},               // 0.1% is 5, floor is 10
		{amount: 10_000, want: 10},              // exactly 0.1%
		{amount: 10_001, want: 11},              // rounds up
		{amount: 500_000, want: 500},            // 0.1%
		{amount: 50_000_000, want: 1_000},       // capped
		{amount: 500_000, user: 600, want: 600}, // higher fee is kept
		{amount: 500_000, user: 10, wantErr: true},
		// With V3 base fee surge
		{amount: 500, base: 25, want: 25},              // small transfer under surge
		{amount: 500_000, base: 25, want: 500},         // 0.1% > base fee
		{amount: 500_000, base: 600, want: 600},        // base fee > 0.1%
		{amount: 10_000_000, base: 10, want: 1_000},    // 1,000 SYN cap preserved
		{amount: 10_000_000, base: 1500, want: 1_500},  // extreme surge above cap
	}
	for _, c := range cases {
		got, err := transferFee(c.amount, c.user, c.base)
		if c.wantErr {
			if err == nil {
				t.Fatalf("amount %d fee %d: expected an error", c.amount, c.user)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("amount %d fee %d base %d: got %d, %v; want %d", c.amount, c.user, c.base, got, err, c.want)
		}
	}
}

func TestValidAddress(t *testing.T) {
	good := "0x" + "0123456789abcdef0123456789ABCDEF01234567"
	if !validAddress(good) {
		t.Fatalf("%s should be valid", good)
	}
	for _, bad := range []string{"", "0x123", "0x" + "g123456789abcdef0123456789abcdef01234567", "123456789abcdef0123456789abcdef0123456789"} {
		if validAddress(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}
