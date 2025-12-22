package common

import "github.com/libp2p/go-libp2p/core/peer"

const (
	// ProtocolID is the p2p protocol used for Client <-> Provider execution
	ProtocolID = "/prxs/rpc/1.0"

	// RegistryProtocolID is the p2p protocol used for Node <-> Registry interactions
	RegistryProtocolID = "/prxs/registry-rpc/1.0"

	// RegistryRendezvous is the DHT Key used ONLY to find the Registry Node.
	// Nodes do NOT advertise services here. They only look for the Registry.
	RegistryRendezvous = "prxs.infra.registry"
)

// --- Data Models ---

type ServiceCard struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Inputs      []string  `json:"inputs"`                 // e.g. ["prompt", "style"]
	CostPerOp   float64   `json:"cost_per_op"`            // Fake tokens
	Version     string    `json:"version"`
	Tags        []string  `json:"tags,omitempty"`        // Categories / labels
	Embedding   []float32 `json:"embedding,omitempty"`   // Optional vector for semantic search
}

// PaymentTicket is an off-chain receipt signed by the client to pay a provider.
type PaymentTicket struct {
	ClientID     string  `json:"client_id"`
	ProviderID   string  `json:"provider_id"`
	Amount       float64 `json:"amount"`
	Nonce        int64   `json:"nonce"`
	Signature    []byte  `json:"signature"`
	ClientPubKey []byte  `json:"client_pubkey"`
}

type StakeProof struct {
	TxHash    string  `json:"tx_hash"`
	Staker    string  `json:"staker"`
	Amount    float64 `json:"amount"`
	Nonce     int64   `json:"nonce"`
	Timestamp int64   `json:"timestamp"`
	ChainID   string  `json:"chain_id"`
	Signature []byte  `json:"signature"`
}

// --- Registry RPC (Node <-> Registry) ---

type RegistryRequest struct {
	Method     string      `json:"method"` // "register" or "find"
	Card       ServiceCard `json:"card,omitempty"`
	Query      string      `json:"query,omitempty"`
	StakeProof *StakeProof `json:"stake_proof,omitempty"`
	// Providers send their own address info so the Registry can tell Clients how to connect
	ProviderInfo *peer.AddrInfo `json:"provider_info,omitempty"`
}

type RegistryResponse struct {
	Success   bool            `json:"success"`
	Providers []peer.AddrInfo `json:"providers,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// --- Execution RPC (Client <-> Provider) ---

type JSONRPCRequest struct {
	Method string      `json:"method"`
	Params interface{} `json:"params"`
	ID     int         `json:"id"`
}

type JSONRPCResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
	ID     int         `json:"id"`
}
