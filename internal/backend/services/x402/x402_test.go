package x402

import (
	"encoding/json"
	"testing"
)

func TestX402ConfigClientAndTracker(t *testing.T) {
	key, err := GeneratePrivateKey()
	if err != nil || key == "" {
		t.Fatalf("GeneratePrivateKey: %v %q", err, key)
	}
	cfg := WalletConfig{PrivateKey: key, Network: NetworkBase}
	if !IsEnabled(cfg) {
		t.Fatal("expected x402 enabled")
	}
	payload, _ := json.Marshal(PaymentRequirement{Scheme: SchemeUSDC, Network: NetworkBase, Amount: "1"})
	req, err := ParsePaymentRequirement(string(payload))
	if err != nil {
		t.Fatalf("ParsePaymentRequirement: %v", err)
	}
	if err := ValidatePaymentRequirement(req, cfg); err != nil {
		t.Fatalf("ValidatePaymentRequirement: %v", err)
	}
	tracker := NewTracker()
	tracker.AddPayment(PaymentRecord{AmountUSD: 1.5})
	if tracker.Count() != 1 || tracker.SessionSpentUSD() != 1.5 {
		t.Fatalf("unexpected tracker state")
	}
}
