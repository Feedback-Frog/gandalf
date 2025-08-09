package gandalf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// rateLimiter handles the internal rate limiting logic
type rateLimiter struct {
	db           *badger.DB
	dataProvider RateLimitDataProvider
	timeProvider TimeProvider
	// Add a buffer pool for JSON encoding/decoding
	jsonPool sync.Pool
	// Add a buffer pool for JSON marshaling/unmarshaling
	bufferPool sync.Pool
}

// newRateLimiter creates a new rate limiter with the provided Badger path
func newRateLimiter(dbPath string) (*rateLimiter, error) {
	// Use a default static provider with 100 requests per second
	dataProvider := &StaticProvider{
		TimeInterval: 100,
		TimeUnit:     "second",
	}
	return newRateLimiterWithProvider(dbPath, dataProvider)
}

// newRateLimiterWithProvider creates a new rate limiter with the provided Badger path and data provider
func newRateLimiterWithProvider(dbPath string, provider RateLimitDataProvider) (*rateLimiter, error) {
	log.Printf("Gandalf.newRateLimiterWithProvider: Creating new rate limiter with custom provider, Badger path: %s", dbPath)
	return newRateLimiterWithProviders(dbPath, provider, &RealTimeProvider{})
}

// newRateLimiterWithProviders creates a new rate limiter with the provided Badger path, data provider, and time provider
func newRateLimiterWithProviders(dbPath string, provider RateLimitDataProvider, timeProvider TimeProvider) (*rateLimiter, error) {
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable Badger's default logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	return &rateLimiter{
		db:           db,
		dataProvider: provider,
		timeProvider: timeProvider,
		jsonPool: sync.Pool{
			New: func() interface{} {
				return &RateLimitData{}
			},
		},
		bufferPool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}, nil
}

// close closes the Badger database
func (r *rateLimiter) close() error {
	return r.db.Close()
}

// peek returns the current limit and limit unit for a given key without consuming a request.
// If the key is not set, it returns 0, "", and ErrKeyNotSet.
func (r *rateLimiter) peek(keyId string) (requestsLeft int, limitUnit string, err error) {
	data := r.jsonPool.Get().(*RateLimitData)
	defer r.jsonPool.Put(data)

	err = r.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(keyId))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return ErrKeyNotSet
			}
			return err
		}

		var dataBytes []byte
		dataBytes, err = item.ValueCopy(nil)
		if err != nil {
			return fmt.Errorf("failed to copy value: %w", err)
		}

		if err := json.Unmarshal(dataBytes, data); err != nil {
			return fmt.Errorf("failed to unmarshal rate limit data: %w", err)
		}

		now := r.timeProvider.Now()
		if !now.Before(data.ResetTime) {
			// If current time is after the reset time, return the full limit and unit, but do not persist anything
			// (matches legacy behavior)
			data.RequestsLeft = data.RateLimit
		}

		return nil
	})

	if err != nil {
		if err == ErrKeyNotSet {
			return 0, "", ErrKeyNotSet
		}
		return 0, "", err
	}

	return data.RequestsLeft, data.RateLimitUnit, nil
}

// purge resets the current limits for a key, or optionally all keys if keyId is empty
func (r *rateLimiter) purge(keyId string) error {
	return r.db.Update(func(txn *badger.Txn) error {
		if keyId == "" {
			// Delete all keys
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				err := txn.Delete(item.Key())
				if err != nil {
					return err
				}
			}
			return nil
		}

		return txn.Delete([]byte(keyId))
	})
}

// limit executes a function with rate limiting and returns (functionResult, functionError, rateLimitError)
// If rate limit is exceeded, returns (nil, nil, ErrRateLimitExceeded)
// If rate limit allows execution, consumes one request and returns function results
func (r *rateLimiter) limit(keyId string, fn func() (any, error)) (any, error, error) {
	requestsLeft, _, err := r.consumeRateLimit(keyId)
	if err != nil {
		if err == ErrRateLimitExceeded {
			return nil, nil, ErrRateLimitExceeded
		}
		return nil, nil, err
	}

	if requestsLeft < 0 {
		return nil, nil, ErrRateLimitExceeded
	}

	result, funcErr := fn()
	return result, funcErr, nil
}

// getResetTime calculates when the rate limit should reset based on the current time and unit
// it returns an error if the unit is invalid
func getResetTime(now time.Time, unit string) (time.Time, error) {
	var resetTime time.Time
	switch unit {
	case "millisecond":
		// Reset at the start of the next millisecond
		resetTime = now.Add(time.Millisecond).Truncate(time.Millisecond)
	case "second":
		// Reset at the start of the next second
		resetTime = now.Add(time.Second).Truncate(time.Second)
	case "minute":
		// Reset at the start of the next minute
		resetTime = now.Add(time.Minute).Truncate(time.Minute)
	case "hour":
		// Reset at the start of the next hour
		resetTime = now.Add(time.Hour).Truncate(time.Hour)
	case "day", "": // Default to day if empty
		// Reset at the start of the next day
		resetTime = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	case "month":
		// Reset at the start of the next month
		resetTime = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, 1, 0)
	default:
		return time.Now(), fmt.Errorf("invalid rate limit unit: %s", unit)
	}

	return resetTime, nil
}

// consumeRateLimit checks and consumes one unit of the rate limit for an API key.
// It mutates state by decrementing the remaining request count.
func (r *rateLimiter) consumeRateLimit(keyId string) (requestsLeft int, limitUnit string, err error) {
	// Get rate limit data from provider
	rateLimitData, err := r.dataProvider.GetRateLimitData(keyId)
	if err != nil {
		return 0, "", fmt.Errorf("failed to get rate limit data: %w", err)
	}

	data := r.jsonPool.Get().(*RateLimitData)
	defer r.jsonPool.Put(data)

	const maxRetries = 11

	for attempt := 0; attempt < maxRetries; attempt++ {
		err = r.db.Update(func(txn *badger.Txn) error {
			item, err := txn.Get([]byte(keyId))
			if err != nil && err != badger.ErrKeyNotFound {
				return err
			}

			if err == badger.ErrKeyNotFound {
				// No data exists for this key, create a new entry
				now := r.timeProvider.Now()
				resetTime, err := getResetTime(now, rateLimitData.RateLimitUnit)
				if err != nil {
					return fmt.Errorf("failed to get reset time: %w", err)
				}
				// Initialize with full limit, then immediately consume 1 request for this call
				*data = RateLimitData{
					RateLimit:     rateLimitData.RateLimit,
					RateLimitUnit: rateLimitData.RateLimitUnit,
					ResetTime:     resetTime,
					LastReset:     now,
					RequestsLeft:  rateLimitData.RateLimit - 1, // Initialize with full limit minus 1 for this request
				}
			} else {
				// Unmarshal existing data
				var dataBytes []byte
				dataBytes, err = item.ValueCopy(nil)
				if err != nil {
					return fmt.Errorf("failed to copy value: %w", err)
				}

				if err := json.Unmarshal(dataBytes, data); err != nil {
					return fmt.Errorf("failed to unmarshal rate limit data: %w", err)
				}
				// Check if we need to reset the limit
				now := r.timeProvider.Now()
				// If current time is at or past the reset time, reset the limit
				if !now.Before(data.ResetTime) {
					resetTime, err := getResetTime(now, rateLimitData.RateLimitUnit)
					if err != nil {
						return fmt.Errorf("failed to get reset time: %w", err)
					}
					data.ResetTime = resetTime
					data.LastReset = now
					// Reset to full limit, then immediately consume 1 request for this call
					data.RequestsLeft = rateLimitData.RateLimit - 1
				} else {
					// Decrement requests left if there are any left
					if data.RequestsLeft > 0 {
						data.RequestsLeft--
					} else {
						return ErrRateLimitExceeded
					}
				}
			}

			// Get a buffer from the pool
			buf := r.bufferPool.Get().(*bytes.Buffer)
			defer r.bufferPool.Put(buf)
			buf.Reset()

			// Use the buffer for JSON encoding
			encoder := json.NewEncoder(buf)
			if err := encoder.Encode(data); err != nil {
				return fmt.Errorf("failed to marshal rate limit data: %w", err)
			}

			// Copy the buffer contents to avoid the trailing newline
			updatedData := make([]byte, buf.Len()-1)
			copy(updatedData, buf.Bytes()[:buf.Len()-1])

			return txn.Set([]byte(keyId), updatedData)
		})

		if err == nil {
			// Success, break out of retry loop
			break
		}

		// If this is a rate limit exceeded error, don't retry - return it immediately
		if err == ErrRateLimitExceeded {
			return 0, rateLimitData.RateLimitUnit, ErrRateLimitExceeded
		}

		// Check if this is a transaction conflict that we should retry
		if err != nil && err.Error() == "Transaction Conflict. Please retry" {
			if attempt < maxRetries-1 {
				// More aggressive exponential backoff with jitter: 0.33ms, 0.66ms, 1.33ms, 2.66ms, 5.33ms, 10.66ms, 21.33ms, 42.66ms, 85.33ms, 170.66ms, 341.33ms
				baseBackoff := time.Duration(1<<attempt) * time.Millisecond / 3
				// Add jitter (±25%) to reduce thundering herd
				jitter := time.Duration(float64(baseBackoff) * 0.25 * (float64(time.Now().UnixNano()%100) / 100.0))
				backoff := baseBackoff + jitter
				time.Sleep(backoff)
				continue
			}
		}

		// For any other error or if we've exhausted retries, return the error
		if err != nil {
			// Check if this was a transaction conflict that we exhausted retries on
			if err.Error() == "Transaction Conflict. Please retry" {
				return 0, "", ErrTransactionConflict
			}
			// For other database errors, return a generic database error
			return 0, "", ErrDatabaseError
		}
	}

	if err != nil {
		if err == ErrRateLimitExceeded {
			return 0, rateLimitData.RateLimitUnit, ErrRateLimitExceeded
		}
		return 0, "", err
	}

	return data.RequestsLeft, data.RateLimitUnit, nil
}
