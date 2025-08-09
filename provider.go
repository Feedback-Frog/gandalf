package gandalf

import (
	"fmt"
)

// StaticProvider implements RateLimitDataProvider for static rate limits
// Exported fields for struct literal usage
type StaticProvider struct {
	TimeInterval int
	TimeUnit     string
}

func (s StaticProvider) GetRateLimitData(key string) (RateLimitData, error) {
	return RateLimitData{
		RateLimit:     s.TimeInterval,
		RateLimitUnit: s.TimeUnit,
	}, nil
}

// RateLimitFetcher is a function type that fetches rate limit data for a given key
type RateLimitFetcher func(key string) (int, string, error)

// DataProvider implements RateLimitDataProvider for database-backed rate limits
type DataProvider struct {
	FetchRateLimit RateLimitFetcher
}

// NewDataProvider creates a new DataProvider with the given rate limit fetcher function
func NewDataProvider(fetcher RateLimitFetcher) *DataProvider {
	return &DataProvider{
		FetchRateLimit: fetcher,
	}
}

func (d DataProvider) GetRateLimitData(key string) (RateLimitData, error) {
	rateLimit, rateLimitUnit, err := d.FetchRateLimit(key)
	if err != nil {
		return RateLimitData{}, fmt.Errorf("failed to fetch rate limit data: %w", err)
	}

	return RateLimitData{
		RateLimit:     rateLimit,
		RateLimitUnit: rateLimitUnit,
	}, nil
}
