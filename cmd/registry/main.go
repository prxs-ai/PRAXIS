package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	ma "github.com/multiformats/go-multiaddr"

	"prxs/common"
	"prxs/storage"
)

// RegistrationRecord tracks an active provider session.
type RegistrationRecord struct {
	LastSeen    time.Time
	ServiceCard common.ServiceCard
	StakeProof  *common.StakeProof
	AddrInfo    peer.AddrInfo
}

// RegistryNode holds the state of the marketplace
type RegistryNode struct {
	Host host.Host

	// Core state: PeerID -> active session
	Registrations map[peer.ID]*RegistrationRecord
	// Lookup index: ServiceName -> PeerIDs
	ServiceIndex map[string][]peer.ID

	mu sync.Mutex

	minStake        float64
	seenStakeNonces map[string]bool // Anti-replay for NEW stakes
	stakeMu         sync.Mutex

	qdrant  *QdrantClient
	storage *storage.RedisStorage
}

func main() {
	// Structured timestamps for all logs
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	port := flag.Int("port", 4001, "port")
	apiPort := flag.Int("api-port", 8080, "REST API port (default: 8080, avoid restricted ports like 6000)")
	bootstrap := flag.String("bootstrap", "", "bootstrap multiaddr")
	keyFile := flag.String("key", "", "path to key file (e.g. registry.key)")
	devMode := flag.Bool("dev", true, "Enable LAN/Dev mode")
	minStake := flag.Float64("min-stake", 10.0, "minimum stake required to register")
	qdrantEnabled := flag.Bool("qdrant-enabled", false, "enable Qdrant semantic index")
	qdrantURL := flag.String("qdrant-url", "http://localhost:6333", "Qdrant base URL")
	qdrantCollection := flag.String("qdrant-collection", "prxs_services", "Qdrant collection name")
	redisAddr := flag.String("redis", "", "Redis address (e.g., localhost:6379) - if set, registrations are stored in both memory and Redis")
	flag.Parse()

	// Load Key if specified, otherwise generate ephemeral
	var privKey crypto.PrivKey
	var err error

	if *keyFile != "" {
		privKey, err = common.LoadOrGenerateKey(*keyFile)
		if err != nil {
			log.Fatalf("Failed to load key: %v", err)
		}
	} else {
		// Ephemeral key
		privKey, _, _ = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
	}

	startRegistry(*port, *apiPort, *bootstrap, *devMode, *minStake, privKey, *qdrantURL, *qdrantCollection, *qdrantEnabled, *redisAddr)
}

func startRegistry(port int, apiPort int, bootstrapAddr string, devMode bool, minStake float64, privKey crypto.PrivKey, qdrantURL, qdrantCollection string, qdrantEnabled bool, redisAddr string) {
	ctx := context.Background()

	h, err := libp2p.New(common.CommonLibp2pOptions(port, privKey)...)
	if err != nil {
		log.Fatal(err)
	}

	var qdrant *QdrantClient
	if qdrantEnabled && qdrantURL != "" && qdrantCollection != "" {
		qdrant = NewQdrantClient(qdrantURL, qdrantCollection)
		fmt.Printf("[Reg] Qdrant enabled: url=%s collection=%s\n", qdrantURL, qdrantCollection)
	}

	// Initialize Redis storage if address is provided
	redisStorage, err := storage.NewRedisStorage(redisAddr, 120*time.Second)
	if err != nil {
		log.Fatalf("[Reg] Failed to initialize Redis storage: %v", err)
	}

	reg := &RegistryNode{
		Host:            h,
		Registrations:   make(map[peer.ID]*RegistrationRecord),
		ServiceIndex:    make(map[string][]peer.ID),
		minStake:        minStake,
		seenStakeNonces: make(map[string]bool),
		qdrant:          qdrant,
		storage:         redisStorage,
	}

	// Restore state from Redis if enabled
	if redisStorage != nil {
		if err := reg.restoreStateFromRedis(ctx); err != nil {
			log.Printf("[Reg] Warning: Failed to restore state from Redis: %v", err)
		}
	}

	// Set Stream Handler for Registry Interactions
	h.SetStreamHandler(common.RegistryProtocolID, reg.handleStream)

	// Setup DHT to advertise "I AM THE REGISTRY"
	peers := []string{}
	if bootstrapAddr != "" {
		peers = append(peers, bootstrapAddr)
	}

	kademliaDHT, err := common.SetupDHT(ctx, h, peers, devMode)
	if err != nil {
		log.Fatal(err)
	}

	// Advertise existence so Providers/Clients can find us
	fmt.Println("REGISTRY ONLINE.")
	common.PrintMyAddresses(h)

	go func() {
		rd := routing.NewRoutingDiscovery(kademliaDHT)
		for {
			// We advertise on the INFRASTRUCTURE key, not a service key
			dutil.Advertise(ctx, rd, common.RegistryRendezvous)
			log.Println("[Reg] Advertised presence on DHT")
			time.Sleep(1 * time.Minute)
		}
	}()

	// Garbage Collection Loop (Remove dead providers)
	go reg.gcLoop()

	// Start REST API server
	go func() {
		router := reg.setupRESTAPI()
		apiPortStr := fmt.Sprintf(":%d", apiPort)
		fmt.Printf("[Reg] Starting REST API server on %s\n", apiPortStr)
		if err := router.Run(apiPortStr); err != nil {
			log.Printf("[Reg] REST API server error: %v\n", err)
		}
	}()

	select {}
}

// gcLoop removes providers who haven't sent a heartbeat in 90 seconds.
func (r *RegistryNode) gcLoop() {
	ticker := time.NewTicker(10 * time.Second)
	for range ticker.C {
		r.mu.Lock()
		now := time.Now()
		for pid, record := range r.Registrations {
			if now.Sub(record.LastSeen) > 90*time.Second {
				log.Printf("[Reg] 💀 Pruning dead provider: %s (last seen %s)\n", pid.ShortString(), record.LastSeen.Format(time.RFC3339))
				delete(r.Registrations, pid)
				r.removeFromIndex(pid, record.ServiceCard.Name)

				// Also delete from Redis if enabled
				if err := r.storage.DeleteRegistration(context.Background(), pid, record.ServiceCard.Name); err != nil {
					log.Printf("[Reg] Warning: Failed to delete registration from Redis: %v", err)
				}
			}
		}
		r.mu.Unlock()
	}
}

// --- Index helpers ---

func (r *RegistryNode) addToIndex(pid peer.ID, serviceName string) {
	list := r.ServiceIndex[serviceName]
	for _, id := range list {
		if id == pid {
			return
		}
	}
	r.ServiceIndex[serviceName] = append(list, pid)
}

func (r *RegistryNode) removeFromIndex(pid peer.ID, serviceName string) {
	list := r.ServiceIndex[serviceName]
	newList := make([]peer.ID, 0, len(list))
	for _, id := range list {
		if id != pid {
			newList = append(newList, id)
		}
	}
	r.ServiceIndex[serviceName] = newList
}

// --- Storage conversion helpers ---

// convertToStorageRecord converts main.RegistrationRecord to storage.RegistrationRecord
func (r *RegistryNode) convertToStorageRecord(record *RegistrationRecord) *storage.RegistrationRecord {
	if record == nil {
		return nil
	}
	return &storage.RegistrationRecord{
		LastSeen:    record.LastSeen,
		ServiceCard: record.ServiceCard,
		StakeProof:  record.StakeProof,
		AddrInfo:    record.AddrInfo,
	}
}

// convertFromStorageRecord converts storage.RegistrationRecord to main.RegistrationRecord
func (r *RegistryNode) convertFromStorageRecord(record *storage.RegistrationRecord) *RegistrationRecord {
	if record == nil {
		return nil
	}
	return &RegistrationRecord{
		LastSeen:    record.LastSeen,
		ServiceCard: record.ServiceCard,
		StakeProof:  record.StakeProof,
		AddrInfo:    record.AddrInfo,
	}
}

// restoreStateFromRedis restores the registry state from Redis on startup.
// It loads all registrations and rebuilds the ServiceIndex.
func (r *RegistryNode) restoreStateFromRedis(ctx context.Context) error {
	if r.storage == nil {
		return nil
	}

	log.Println("[Reg] Restoring state from Redis...")

	// Load all registrations from Redis
	storageRecords, err := r.storage.RestoreAllRegistrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to restore registrations: %v", err)
	}

	if len(storageRecords) == 0 {
		log.Println("[Reg] No registrations found in Redis")
		return nil
	}

	// Convert storage records to main records and rebuild the index
	r.mu.Lock()
	defer r.mu.Unlock()

	for pid, storageRecord := range storageRecords {
		record := r.convertFromStorageRecord(storageRecord)
		r.Registrations[pid] = record
		r.addToIndex(pid, record.ServiceCard.Name)
	}

	log.Printf("[Reg] ✅ State restored: %d active registrations", len(r.Registrations))

	// Log service summary
	serviceCount := len(r.ServiceIndex)
	if serviceCount > 0 {
		log.Printf("[Reg] Services available: %d unique services", serviceCount)
	}

	return nil
}

// checkStakeValidity verifies signature and amount, but DOES NOT check replay/nonce.
// This is used for both new registrations and verifying stored heartbeats.
func (r *RegistryNode) checkStakeValidity(remote peer.ID, proof *common.StakeProof) error {
	if proof == nil {
		return fmt.Errorf("stake proof required (min %.2f)", r.minStake)
	}

	if proof.Amount < r.minStake {
		return fmt.Errorf("stake too low: have %.2f need %.2f", proof.Amount, r.minStake)
	}

	if proof.Staker != "" && proof.Staker != remote.String() {
		return fmt.Errorf("stake staker mismatch (expected %s got %s)", remote.ShortString(), proof.Staker)
	}

	pubKey := r.Host.Peerstore().PubKey(remote)
	if pubKey == nil {
		return fmt.Errorf("missing pubkey for %s", remote.ShortString())
	}

	payload := fmt.Sprintf("%s|%f|%d|%d|%s", proof.TxHash, proof.Amount, proof.Nonce, proof.Timestamp, proof.ChainID)
	digest := sha256.Sum256([]byte(payload))
	if ok, err := pubKey.Verify(digest[:], proof.Signature); err != nil || !ok {
		if err != nil {
			return fmt.Errorf("stake signature verify failed: %v", err)
		}
		return fmt.Errorf("stake signature invalid")
	}

	return nil
}

func (r *RegistryNode) handleStream(stream network.Stream) {
	defer stream.Close()
	rw := bufio.NewReadWriter(bufio.NewReader(stream), bufio.NewWriter(stream))

	var req common.RegistryRequest
	if err := json.NewDecoder(rw).Decode(&req); err != nil {
		return
	}

	resp := common.RegistryResponse{Success: false}
	remotePeer := stream.Conn().RemotePeer()

	switch req.Method {
	case "register":
		// Decide whether this is a new registration or a heartbeat
		r.mu.Lock()
		existing, isRegistered := r.Registrations[remotePeer]
		r.mu.Unlock()

		isHeartbeat := false
		if isRegistered && existing.StakeProof != nil && req.StakeProof != nil &&
			existing.StakeProof.TxHash == req.StakeProof.TxHash {
			isHeartbeat = true
		}

		if isHeartbeat {
			// Heartbeat: update LastSeen and optionally AddrInfo
			r.mu.Lock()
			if entry, ok := r.Registrations[remotePeer]; ok {
				entry.LastSeen = time.Now()
				if req.ProviderInfo != nil {
					entry.AddrInfo = *req.ProviderInfo
				}
				log.Printf("[Reg] ❤️ Heartbeat received: %s\n", remotePeer.ShortString())
				resp.Success = true

				// Save to Redis if enabled
				storageRecord := r.convertToStorageRecord(entry)
				if err := r.storage.SaveRegistration(context.Background(), remotePeer, storageRecord); err != nil {
					log.Printf("[Reg] Warning: Failed to save heartbeat to Redis: %v", err)
				}
			}
			r.mu.Unlock()
		} else {
			// New registration or stake changed
			if err := r.checkStakeValidity(remotePeer, req.StakeProof); err != nil {
				resp.Error = err.Error()
				log.Printf("[Reg] ❌ Stake Invalid: %v\n", err)
				break
			}

			// Replay protection only for NEW stake proofs
			key := fmt.Sprintf("%s|%d", req.StakeProof.TxHash, req.StakeProof.Nonce)
			r.stakeMu.Lock()
			if r.seenStakeNonces[key] {
				r.stakeMu.Unlock()
				resp.Error = "stake proof already used (replay detected)"
				log.Printf("[Reg] ❌ Replay Attack: %s\n", resp.Error)
				break
			}
			r.seenStakeNonces[key] = true
			r.stakeMu.Unlock()

			// Compute / fill embedding for this service (for semantic search / Qdrant)
			embedding := buildServiceEmbedding(req.Card)

			r.mu.Lock()

			// Remove old index entry if service name changed
			if isRegistered && existing.ServiceCard.Name != req.Card.Name {
				r.removeFromIndex(remotePeer, existing.ServiceCard.Name)
			}

			if req.ProviderInfo != nil {
				newRecord := &RegistrationRecord{
					LastSeen:    time.Now(),
					ServiceCard: req.Card,
					StakeProof:  req.StakeProof,
					AddrInfo:    *req.ProviderInfo,
				}
				r.Registrations[remotePeer] = newRecord
				r.addToIndex(remotePeer, req.Card.Name)

				// Save to Redis if enabled
				storageRecord := r.convertToStorageRecord(newRecord)
				if err := r.storage.SaveRegistration(context.Background(), remotePeer, storageRecord); err != nil {
					log.Printf("[Reg] Warning: Failed to save registration to Redis: %v", err)
				}
			}

			log.Printf("[Reg] ✅ New Registration: %s (Service: %s)\n", remotePeer.ShortString(), req.Card.Name)
			resp.Success = true

			r.mu.Unlock()

			// Optional: index in Qdrant for semantic search
			if r.qdrant != nil && len(embedding) > 0 {
				payload := map[string]interface{}{
					"service_name": req.Card.Name,
					"peer_id":      remotePeer.String(),
					"description":  req.Card.Description,
					"tags":         req.Card.Tags,
					"version":      req.Card.Version,
					"cost_per_op":  req.Card.CostPerOp,
				}
				pointID := fmt.Sprintf("%s:%s", remotePeer.String(), req.Card.Name)
				if err := r.qdrant.UpsertService(pointID, embedding, payload); err != nil {
					log.Printf("[Reg] Qdrant upsert error: %v\n", err)
				}
			}
		}

	case "find":
		r.mu.Lock()
		results := []peer.AddrInfo{}
		query := strings.ToLower(req.Query)

		for name, peerIDs := range r.ServiceIndex {
			if strings.Contains(strings.ToLower(name), query) {
				for _, pid := range peerIDs {
					if reg, ok := r.Registrations[pid]; ok {
						results = append(results, reg.AddrInfo)
					}
				}
			}
		}
		resp.Providers = results
		resp.Success = true
		log.Printf("[Reg] Served query '%s' -> %d providers\n", req.Query, len(results))
		r.mu.Unlock()

	default:
		resp.Error = "Unknown method"
	}

	_ = json.NewEncoder(rw).Encode(resp)
	_ = rw.Flush()
}

// setupRESTAPI configures the Gin router with read-only endpoints for Services
func (r *RegistryNode) setupRESTAPI() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	// Enable CORS for frontend
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:3000", "http://127.0.0.1:5173"},
		AllowMethods:     []string{"GET", "POST", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api/v1")
	{
		api.GET("/services", r.getAllServices)
		api.GET("/services_full", r.getAllServicesFull)

		// GET services by name (query parameter)
		api.GET("/services/search", r.searchServices)

		// GET specific service by exact name
		api.GET("/services/:name", r.getServiceByName)

		// GET semantic search (optional; Qdrant-backed)
		api.GET("/services/semantic_search", r.semanticSearchServices)

		// GET registry info (Peer ID and multiaddr)
		api.GET("/registry/info", r.getRegistryInfo)
	}

	return router
}

// getAllServices returns all registered services
func (r *RegistryNode) getAllServices(c *gin.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	view := make(map[string][]peer.AddrInfo)
	for _, reg := range r.Registrations {
		name := reg.ServiceCard.Name
		view[name] = append(view[name], reg.AddrInfo)
	}

	c.JSON(http.StatusOK, gin.H{
		"services": view,
		"count":    len(view),
	})
}

// getAllServicesFull returns all services with their ServiceCard and providers.
func (r *RegistryNode) getAllServicesFull(c *gin.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	view := make(map[string]gin.H)
	for _, reg := range r.Registrations {
		name := reg.ServiceCard.Name
		entry, ok := view[name]
		if !ok {
			entry = gin.H{
				"card":      reg.ServiceCard,
				"providers": []peer.AddrInfo{},
			}
		}
		providers := entry["providers"].([]peer.AddrInfo)
		providers = append(providers, reg.AddrInfo)
		entry["providers"] = providers
		view[name] = entry
	}

	c.JSON(http.StatusOK, gin.H{
		"services": view,
		"count":    len(view),
	})
}

// searchServices searches for services by name (partial match)
func (r *RegistryNode) searchServices(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "query parameter 'q' is required",
		})
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	results := make(map[string][]peer.AddrInfo)
	queryLower := strings.ToLower(query)

	for name, peerIDs := range r.ServiceIndex {
		if strings.Contains(strings.ToLower(name), queryLower) {
			for _, pid := range peerIDs {
				if reg, ok := r.Registrations[pid]; ok {
					results[name] = append(results[name], reg.AddrInfo)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"query":    query,
		"services": results,
		"count":    len(results),
	})
}

// getServiceByName returns providers for a specific service name
func (r *RegistryNode) getServiceByName(c *gin.Context) {
	serviceName := c.Param("name")

	r.mu.Lock()
	defer r.mu.Unlock()

	providers := []peer.AddrInfo{}
	if peerIDs, ok := r.ServiceIndex[serviceName]; ok {
		for _, pid := range peerIDs {
			if reg, ok := r.Registrations[pid]; ok {
				providers = append(providers, reg.AddrInfo)
			}
		}
	}

	if len(providers) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("service '%s' not found", serviceName),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service":   serviceName,
		"providers": providers,
		"count":     len(providers),
	})
}

// --- Semantic search (Qdrant-backed, optional) ---

const defaultEmbeddingDim = 64

// buildServiceEmbedding returns a vector for this card.
func buildServiceEmbedding(card common.ServiceCard) []float32 {
	textParts := []string{card.Name, card.Description}
	if len(card.Tags) > 0 {
		textParts = append(textParts, strings.Join(card.Tags, " "))
	}
	text := strings.ToLower(strings.Join(textParts, " "))
	return simpleTextEmbedding(text, defaultEmbeddingDim)
}

// simpleTextEmbedding builds a very simple bag-of-runes embedding.
// This is only for demo purposes; in production you'd plug a real embedding model.
func simpleTextEmbedding(text string, dim int) []float32 {
	if dim <= 0 {
		dim = defaultEmbeddingDim
	}
	vec := make([]float32, dim)
	for _, r := range text {
		idx := int(r) % dim
		if idx < 0 {
			idx += dim
		}
		vec[idx] += 1.0
	}
	return vec
}

// semanticSearchServices exposes a Qdrant-backed semantic search endpoint.
// GET /api/v1/services/semantic_search?q=...&k=5
func (r *RegistryNode) semanticSearchServices(c *gin.Context) {
	if r.qdrant == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "semantic search is not enabled (no Qdrant configured)",
		})
		return
	}

	query := c.Query("q")
	if strings.TrimSpace(query) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "query parameter 'q' is required",
		})
		return
	}

	kStr := c.DefaultQuery("k", "5")
	k, err := strconv.Atoi(kStr)
	if err != nil || k <= 0 {
		k = 5
	}

	vector := simpleTextEmbedding(strings.ToLower(query), defaultEmbeddingDim)
	results, err := r.qdrant.Search(vector, k)
	if err != nil {
		log.Printf("[Reg] Qdrant search error: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("qdrant search failed: %v", err),
		})
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	type apiResult struct {
		ServiceName string             `json:"service_name"`
		Score       float64            `json:"score"`
		Card        common.ServiceCard `json:"card"`
		Providers   []peer.AddrInfo    `json:"providers"`
	}

	apiResults := make([]apiResult, 0, len(results))

	for _, hit := range results {
		payload := hit.Payload
		serviceName, _ := payload["service_name"].(string)
		peerIDStr, _ := payload["peer_id"].(string)
		if serviceName == "" || peerIDStr == "" {
			continue
		}

		pid, err := peer.Decode(peerIDStr)
		if err != nil {
			continue
		}

		reg, ok := r.Registrations[pid]
		if !ok || reg.ServiceCard.Name != serviceName {
			continue
		}

		apiResults = append(apiResults, apiResult{
			ServiceName: serviceName,
			Score:       hit.Score,
			Card:        reg.ServiceCard,
			Providers:   []peer.AddrInfo{reg.AddrInfo},
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"query":   query,
		"results": apiResults,
		"count":   len(apiResults),
	})
}

// getRegistryInfo returns the registry's Peer ID and multiaddrs
// GET /api/v1/registry/info
func (r *RegistryNode) getRegistryInfo(c *gin.Context) {
	addrs := make([]string, 0, len(r.Host.Addrs()))
	for _, addr := range r.Host.Addrs() {
		addrs = append(addrs, fmt.Sprintf("%s/p2p/%s", addr, r.Host.ID()))
	}

	// Find UDP port for bootstrap (prefer QUIC)
	var bootstrapAddr string
	for _, addr := range r.Host.Addrs() {
		protocols := addr.Protocols()
		if len(protocols) > 0 && protocols[0].Code == ma.P_IP4 {
			port, err := addr.ValueForProtocol(ma.P_UDP)
			if err != nil {
				port, _ = addr.ValueForProtocol(ma.P_TCP)
			}
			if port != "" {
				bootstrapAddr = fmt.Sprintf("/ip4/127.0.0.1/udp/%s/quic-v1/p2p/%s", port, r.Host.ID())
				break
			}
		}
	}
	if bootstrapAddr == "" && len(addrs) > 0 {
		// Fallback to first addr
		bootstrapAddr = addrs[0]
	}

	c.JSON(http.StatusOK, gin.H{
		"peer_id":    r.Host.ID().String(),
		"multiaddrs": addrs,
		"bootstrap":  bootstrapAddr,
	})
}

// --- Minimal Qdrant HTTP client (for demo use only) ---

type QdrantClient struct {
	BaseURL    string
	Collection string
	HTTP       *http.Client
	VectorSize int
}

func NewQdrantClient(baseURL, collection string) *QdrantClient {
	return &QdrantClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Collection: collection,
		HTTP:       &http.Client{Timeout: 5 * time.Second},
	}
}

// ensureCollection creates the collection if it does not exist yet.
func (qc *QdrantClient) ensureCollection(dim int) error {
	if qc == nil {
		return nil
	}
	if qc.VectorSize != 0 {
		return nil
	}

	body := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     dim,
			"distance": "Cosine",
		},
	}

	b, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/collections/%s", qc.BaseURL, qc.Collection)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := qc.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 200 OK or 201 Created or 409 Already exists are fine
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("qdrant create collection failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	qc.VectorSize = dim
	return nil
}

// UpsertService stores or updates a single service vector in Qdrant.
func (qc *QdrantClient) UpsertService(id string, vector []float32, payload map[string]interface{}) error {
	if qc == nil {
		return nil
	}
	if len(vector) == 0 {
		return nil
	}
	if err := qc.ensureCollection(len(vector)); err != nil {
		return err
	}

	// Qdrant in this config expects numeric or UUID IDs.
	// We hash the provided string into a uint64 for demo purposes.
	numID := hashToUint64(id)

	body := map[string]interface{}{
		"points": []map[string]interface{}{
			{
				"id":      numID,
				"vector":  vector,
				"payload": payload,
			},
		},
	}

	b, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/collections/%s/points", qc.BaseURL, qc.Collection)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := qc.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("qdrant upsert failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// hashToUint64 deterministically maps a string to a uint64.
func hashToUint64(s string) uint64 {
	sum := sha256.Sum256([]byte(s))
	var v uint64
	for i := 0; i < 8; i++ {
		v = (v << 8) | uint64(sum[i])
	}
	return v
}

type qdrantSearchResult struct {
	ID      interface{}            `json:"id"`
	Score   float64                `json:"score"`
	Payload map[string]interface{} `json:"payload"`
}

type qdrantSearchResponse struct {
	Result []qdrantSearchResult `json:"result"`
}

// Search performs a vector similarity search in Qdrant.
func (qc *QdrantClient) Search(vector []float32, limit int) ([]qdrantSearchResult, error) {
	if qc == nil {
		return nil, fmt.Errorf("qdrant client not configured")
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("empty query vector")
	}
	if limit <= 0 {
		limit = 5
	}
	if err := qc.ensureCollection(len(vector)); err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
	}

	b, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/collections/%s/points/search", qc.BaseURL, qc.Collection)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := qc.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("qdrant search failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	var sr qdrantSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, err
	}
	return sr.Result, nil
}
