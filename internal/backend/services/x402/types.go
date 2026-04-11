package x402

type PaymentNetwork string
type PaymentScheme string

const (
	NetworkBase PaymentNetwork = "base"
	SchemeUSDC  PaymentScheme  = "usdc"
)

type WalletConfig struct {
	PrivateKey      string         `json:"private_key,omitempty"`
	Address         string         `json:"address,omitempty"`
	Network         PaymentNetwork `json:"network,omitempty"`
	MaxPayment      float64        `json:"max_payment,omitempty"`
	MaxSessionSpend float64        `json:"max_session_spend,omitempty"`
}

type PaymentRequirement struct {
	Scheme   PaymentScheme  `json:"scheme"`
	Amount   string         `json:"amount"`
	Network  PaymentNetwork `json:"network"`
	Resource string         `json:"resource,omitempty"`
}

type PaymentRecord struct {
	Resource  string  `json:"resource"`
	AmountUSD float64 `json:"amount_usd"`
}
