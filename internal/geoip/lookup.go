package geoip

import (
	"fmt"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

type LocationInfo struct {
	Country string  `json:"country"`
	City    string  `json:"city"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type Lookup struct {
	mu     sync.RWMutex
	reader *geoip2.Reader
}

func New(dbPath string) (*Lookup, error) {
	if dbPath == "" {
		return &Lookup{}, nil
	}
	r, err := geoip2.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("geoip: open db: %w", err)
	}
	return &Lookup{reader: r}, nil
}

func (l *Lookup) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reader != nil {
		return l.reader.Close()
	}
	return nil
}

func (l *Lookup) Lookup(ipStr string) *LocationInfo {
	l.mu.RLock()
	r := l.reader
	l.mu.RUnlock()

	if r == nil || ipStr == "" {
		return nil
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}

	rec, err := r.City(ip)
	if err != nil {
		return nil
	}

	city := ""
	if len(rec.City.Names) > 0 {
		city = rec.City.Names["en"]
	}

	return &LocationInfo{
		Country: rec.Country.IsoCode,
		City:    city,
		Lat:     rec.Location.Latitude,
		Lon:     rec.Location.Longitude,
	}
}
