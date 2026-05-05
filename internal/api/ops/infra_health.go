package ops

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btopcu/argus/internal/apierr"
)

type dbPool struct {
	Max           int32 `json:"max"`
	InUse         int32 `json:"in_use"`
	Idle          int32 `json:"idle"`
	Waiting       int32 `json:"waiting"`
	AcquiredTotal int64 `json:"acquired_total"`
}

type tableSize struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	RowEstimate int64  `json:"row_estimate"`
}

type partitionInfo struct {
	Parent    string `json:"parent"`
	Child     string `json:"child"`
	RangeFrom string `json:"range_from,omitempty"`
	RangeTo   string `json:"range_to,omitempty"`
}

type dbBlock struct {
	Pool           dbPool          `json:"pool"`
	Tables         []tableSize     `json:"tables"`
	Partitions     []partitionInfo `json:"partitions"`
	ReplicationLag *float64        `json:"replication_lag_seconds"`
	Error          string          `json:"error,omitempty"`
}

type redisBlock struct {
	OpsPerSec        float64   `json:"ops_per_sec"`
	HitRate          float64   `json:"hit_rate"`
	MissRate         float64   `json:"miss_rate"`
	MemoryUsedBytes  int64     `json:"memory_used_bytes"`
	MemoryMaxBytes   int64     `json:"memory_max_bytes"`
	Evictions        int64     `json:"evictions_total"`
	ConnectedClients int64     `json:"connected_clients"`
	LatencyP99Ms     float64   `json:"latency_p99_ms"`
	KeysByDB         []redisDB `json:"keys_by_db"`
	Error            string    `json:"error,omitempty"`
}

type redisDB struct {
	DB      int   `json:"db"`
	Keys    int64 `json:"keys"`
	Expires int64 `json:"expires"`
}

type consumerLag struct {
	Consumer     string `json:"consumer"`
	Pending      uint64 `json:"pending"`
	AckPending   int    `json:"ack_pending"`
	Redeliveries uint64 `json:"redeliveries"`
	Slow         bool   `json:"slow"`
}

type natsStream struct {
	Name        string        `json:"name"`
	Subjects    []string      `json:"subjects"`
	Messages    uint64        `json:"messages"`
	Bytes       uint64        `json:"bytes"`
	Consumers   int           `json:"consumers"`
	ConsumerLag []consumerLag `json:"consumer_lag"`
}

type natsBlock struct {
	Streams  []natsStream `json:"streams"`
	DLQDepth int          `json:"dlq_depth"`
	Error    string       `json:"error,omitempty"`
}

type infraHealthResponse struct {
	DB    dbBlock    `json:"db"`
	Redis redisBlock `json:"redis"`
	NATS  natsBlock  `json:"nats"`
}

const redisInfoCacheTTL = 5 * time.Second

var (
	redisCacheMu    sync.Mutex
	redisCachedAt   time.Time
	redisCachedInfo redisBlock
)

var knownStreams = []string{"EVENTS", "JOBS"}

func (h *Handler) InfraHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var resp infraHealthResponse
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		resp.DB = h.fetchDBHealth(ctx)
	}()

	go func() {
		defer wg.Done()
		resp.Redis = h.fetchRedisHealth(ctx)
	}()

	go func() {
		defer wg.Done()
		resp.NATS = h.fetchNATSHealth(ctx)
	}()

	wg.Wait()
	apierr.WriteSuccess(w, http.StatusOK, resp)
}

func (h *Handler) fetchDBHealth(ctx context.Context) dbBlock {
	if h.pgPool == nil {
		return dbBlock{Error: "db pool not configured"}
	}

	stat := h.pgPool.Stat()
	block := dbBlock{
		Pool: dbPool{
			Max:   stat.MaxConns(),
			InUse: stat.AcquiredConns(),
			Idle:  stat.IdleConns(),
		},
	}

	tableRows, err := h.pgPool.Query(ctx, `
		SELECT relname,
		       pg_total_relation_size(c.oid) AS size_bytes,
		       GREATEST(reltuples::bigint, 0) AS row_estimate
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind = 'r'
		  AND n.nspname = 'public'
		ORDER BY size_bytes DESC
		LIMIT 10
	`)
	if err != nil {
		block.Error = "failed to query table sizes: " + err.Error()
		return block
	}
	defer tableRows.Close()

	for tableRows.Next() {
		var ts tableSize
		if err := tableRows.Scan(&ts.Name, &ts.SizeBytes, &ts.RowEstimate); err == nil {
			block.Tables = append(block.Tables, ts)
		}
	}

	partRows, err := h.pgPool.Query(ctx, `
		SELECT p.relname AS parent, c.relname AS child
		FROM pg_inherits i
		JOIN pg_class p ON p.oid = i.inhparent
		JOIN pg_class c ON c.oid = i.inhrelid
		ORDER BY parent, child
		LIMIT 20
	`)
	if err == nil {
		defer partRows.Close()
		for partRows.Next() {
			var pi partitionInfo
			if err := partRows.Scan(&pi.Parent, &pi.Child); err == nil {
				block.Partitions = append(block.Partitions, pi)
			}
		}
	}

	return block
}

func (h *Handler) fetchRedisHealth(ctx context.Context) redisBlock {
	redisCacheMu.Lock()
	if !redisCachedAt.IsZero() && time.Since(redisCachedAt) < redisInfoCacheTTL {
		cached := redisCachedInfo
		redisCacheMu.Unlock()
		return cached
	}
	redisCacheMu.Unlock()

	if h.redisClient == nil {
		return redisBlock{Error: "redis client not configured"}
	}

	infoStr, err := h.redisClient.Info(ctx, "memory", "stats", "clients", "keyspace").Result()
	if err != nil {
		return redisBlock{Error: "failed to query redis info: " + err.Error()}
	}

	info := parseRedisInfo(infoStr)
	block := redisBlock{
		OpsPerSec:        parseFloat(info["instantaneous_ops_per_sec"]),
		MemoryUsedBytes:  parseInt(info["used_memory"]),
		MemoryMaxBytes:   parseInt(info["maxmemory"]),
		Evictions:        parseInt(info["evicted_keys"]),
		ConnectedClients: parseInt(info["connected_clients"]),
	}

	hits := parseFloat(info["keyspace_hits"])
	misses := parseFloat(info["keyspace_misses"])
	total := hits + misses
	if total > 0 {
		block.HitRate = hits / total
		block.MissRate = misses / total
	}

	for k, v := range info {
		if strings.HasPrefix(k, "db") {
			var dbNum int
			_, _ = parseDBLine(k, v, &dbNum, &block)
		}
	}

	// Only cache on success — Error is empty at this point.
	redisCacheMu.Lock()
	redisCachedInfo = block
	redisCachedAt = time.Now()
	redisCacheMu.Unlock()

	return block
}

func parseDBLine(key, value string, dbNum *int, block *redisBlock) (bool, error) {
	if !strings.HasPrefix(key, "db") {
		return false, nil
	}
	n := 0
	for _, ch := range key[2:] {
		if ch < '0' || ch > '9' {
			return false, nil
		}
		n = n*10 + int(ch-'0')
	}
	*dbNum = n

	var keys, expires int64
	parts := strings.Split(value, ",")
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "keys":
			keys = parseInt(kv[1])
		case "expires":
			expires = parseInt(kv[1])
		}
	}
	block.KeysByDB = append(block.KeysByDB, redisDB{DB: n, Keys: keys, Expires: expires})
	return true, nil
}

func parseRedisInfo(raw string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		result[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
	}
	return result
}

func (h *Handler) fetchNATSHealth(ctx context.Context) natsBlock {
	if h.natsJS == nil {
		return natsBlock{Error: "NATS JetStream not configured"}
	}

	block := natsBlock{}
	for _, name := range knownStreams {
		stream, err := h.natsJS.Stream(ctx, name)
		if err != nil {
			continue
		}
		info, err := stream.Info(ctx)
		if err != nil {
			continue
		}

		ns := natsStream{
			Name:      info.Config.Name,
			Subjects:  info.Config.Subjects,
			Messages:  info.State.Msgs,
			Bytes:     info.State.Bytes,
			Consumers: info.State.Consumers,
		}

		// Enumerate consumers via ListConsumers — returns ConsumerInfo channel.
		lister := stream.ListConsumers(ctx)
		for cInfo := range lister.Info() {
			if cInfo == nil {
				continue
			}
			ns.ConsumerLag = append(ns.ConsumerLag, consumerLag{
				Consumer:     cInfo.Name,
				Pending:      cInfo.NumPending,
				AckPending:   cInfo.NumAckPending,
				Redeliveries: uint64(cInfo.NumRedelivered),
				Slow:         cInfo.NumPending > 1000,
			})
		}
		if lerr := lister.Err(); lerr != nil {
			h.logger.Warn().Err(lerr).Str("stream", name).Msg("consumer list error")
		}
		block.Streams = append(block.Streams, ns)
	}

	return block
}

func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err == nil {
		return n
	}
	// Fall back to float parsing for values like "1.5e6".
	return int64(parseFloat(s))
}
