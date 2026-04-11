package x402

import "sync"

type Tracker struct {
	mu       sync.Mutex
	payments []PaymentRecord
}

func NewTracker() *Tracker { return &Tracker{} }

func (t *Tracker) AddPayment(record PaymentRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.payments = append(t.payments, record)
}

func (t *Tracker) SessionSpentUSD() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total float64
	for _, payment := range t.payments {
		total += payment.AmountUSD
	}
	return total
}

func (t *Tracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.payments)
}

func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.payments = nil
}
