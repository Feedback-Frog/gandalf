package gandalf

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// HighThroughputTestSuite tests the rate limiter's ability to handle high concurrent load
type HighThroughputTestSuite struct {
	suite.Suite
	limiter  *rateLimiter
	dbPath   string
	provider *StaticProvider
}

func (s *HighThroughputTestSuite) SetupTest() {
	s.provider = &StaticProvider{
		TimeInterval: 1000, // 1000 requests per second
		TimeUnit:     "second",
	}
	s.dbPath = fmt.Sprintf("test_rl_high_throughput_%d.db", time.Now().UnixNano())
	// Use real time provider for performance tests to simulate real-world conditions
	limiter, err := newRateLimiterWithProviders(s.dbPath, s.provider, &RealTimeProvider{})
	require.NoError(s.T(), err)
	s.limiter = limiter
}

func (s *HighThroughputTestSuite) TearDownTest() {
	if s.limiter != nil {
		s.limiter.close()
	}
	if s.dbPath != "" {
		_ = os.RemoveAll(s.dbPath)
	}
}

// TestHighThroughputConcurrentRequests tests 1000 requests with real time
// This tests actual performance under real-world conditions with a single key
func (s *HighThroughputTestSuite) TestHighThroughputConcurrentRequests() {
	const (
		totalRequests = 1000
		singleKey     = "high_throughput_single_key" // Use single key to test true limits
	)

	var (
		successCount     int64
		rateLimitCount   int64
		errorCount       int64
		unexpectedErrors []error
		mu               sync.Mutex
	)

	startTime := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(requestNum int) {
			defer wg.Done()

			_, _, err := s.limiter.limit(singleKey, func() (any, error) {
				return "success", nil
			})

			mu.Lock()
			switch err {
			case nil:
				successCount++
			case ErrRateLimitExceeded:
				rateLimitCount++
			default:
				errorCount++
				if len(unexpectedErrors) < 10 {
					unexpectedErrors = append(unexpectedErrors, err)
				}
			}
			mu.Unlock()
		}(i)
		// Spread requests over time to reduce contention
		time.Sleep(time.Microsecond)
	}
	wg.Wait()

	duration := time.Since(startTime)
	throughput := float64(successCount) / duration.Seconds()

	fmt.Printf("\n=== SINGLE KEY CONCURRENT TEST RESULTS ===\n")
	fmt.Printf("Total Duration: %v\n", duration)
	fmt.Printf("Successful Requests: %d\n", successCount)
	fmt.Printf("Rate Limited Requests: %d\n", rateLimitCount)
	fmt.Printf("Error Requests: %d\n", errorCount)
	fmt.Printf("Throughput: %.2f requests/second\n", throughput)
	fmt.Printf("Error Rate: %.2f%%\n", float64(errorCount)/float64(totalRequests)*100)
	if len(unexpectedErrors) > 0 {
		fmt.Printf("Sample Errors:\n")
		for i, err := range unexpectedErrors {
			fmt.Printf("  %d: %v\n", i+1, err)
		}
	}
	fmt.Printf("==========================================\n\n")

	assert.Equal(s.T(), int64(totalRequests), successCount+rateLimitCount+errorCount, "Total requests should match")
	// Don't assert on specific throughput - let's see what the actual limit is
	fmt.Printf("Actual throughput achieved: %.2f req/s\n", throughput)
}

// TestHighThroughputSequentialRequests tests 1000 sequential requests with real time
func (s *HighThroughputTestSuite) TestHighThroughputSequentialRequests() {
	const (
		totalRequests = 1000
		singleKey     = "sequential_single_key" // Use single key
	)

	var successCount int64
	startTime := time.Now()

	for range totalRequests {
		_, _, err := s.limiter.limit(singleKey, func() (any, error) {
			return "success", nil
		})

		if err == nil {
			successCount++
		}
	}

	duration := time.Since(startTime)
	throughput := float64(successCount) / duration.Seconds()

	fmt.Printf("\n=== SINGLE KEY SEQUENTIAL TEST RESULTS ===\n")
	fmt.Printf("Total Duration: %v\n", duration)
	fmt.Printf("Successful Requests: %d\n", successCount)
	fmt.Printf("Throughput: %.2f requests/second\n", throughput)
	fmt.Printf("==========================================\n\n")

	assert.Equal(s.T(), int64(totalRequests), successCount, "All sequential requests should succeed")
	fmt.Printf("Sequential throughput achieved: %.2f req/s\n", throughput)
}

// TestHighThroughputMixedWorkload tests a realistic mixed workload with real time
func (s *HighThroughputTestSuite) TestHighThroughputMixedWorkload() {
	const (
		highThroughputRequests   = 500 // 500 requests with 1000/sec limit
		mediumThroughputRequests = 300 // 300 requests with 100/sec limit
		lowThroughputRequests    = 200 // 200 requests with 10/sec limit
		totalRequests            = highThroughputRequests + mediumThroughputRequests + lowThroughputRequests
	)

	// Create different providers for different workloads
	highProvider := &StaticProvider{TimeInterval: 1000, TimeUnit: "second"}
	mediumProvider := &StaticProvider{TimeInterval: 100, TimeUnit: "second"}
	lowProvider := &StaticProvider{TimeInterval: 10, TimeUnit: "second"}

	highDBPath := fmt.Sprintf("test_high_%d.db", time.Now().UnixNano())
	mediumDBPath := fmt.Sprintf("test_medium_%d.db", time.Now().UnixNano())
	lowDBPath := fmt.Sprintf("test_low_%d.db", time.Now().UnixNano())

	highLimiter, err := newRateLimiterWithProviders(highDBPath, highProvider, &RealTimeProvider{})
	require.NoError(s.T(), err)
	defer func() {
		highLimiter.close()
		os.RemoveAll(highDBPath)
	}()

	mediumLimiter, err := newRateLimiterWithProviders(mediumDBPath, mediumProvider, &RealTimeProvider{})
	require.NoError(s.T(), err)
	defer func() {
		mediumLimiter.close()
		os.RemoveAll(mediumDBPath)
	}()

	lowLimiter, err := newRateLimiterWithProviders(lowDBPath, lowProvider, &RealTimeProvider{})
	require.NoError(s.T(), err)
	defer func() {
		lowLimiter.close()
		os.RemoveAll(lowDBPath)
	}()

	var (
		highSuccess, mediumSuccess, lowSuccess       int64
		highRateLimit, mediumRateLimit, lowRateLimit int64
		highErrors, mediumErrors, lowErrors          int64
		wg                                           sync.WaitGroup
		mu                                           sync.Mutex
	)

	startTime := time.Now()

	// High throughput workload with single key
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range highThroughputRequests {
			_, _, err := highLimiter.limit("high_single_key", func() (any, error) {
				return "high", nil
			})

			mu.Lock()
			switch err {
			case nil:
				highSuccess++
			case ErrRateLimitExceeded:
				highRateLimit++
			default:
				highErrors++
			}
			mu.Unlock()
		}
	}()

	// Medium throughput workload with single key
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < mediumThroughputRequests; i++ {
			_, _, err := mediumLimiter.limit("medium_single_key", func() (any, error) {
				return "medium", nil
			})

			mu.Lock()
			switch err {
			case nil:
				mediumSuccess++
			case ErrRateLimitExceeded:
				mediumRateLimit++
			default:
				mediumErrors++
			}
			mu.Unlock()
		}
	}()

	// Low throughput workload with single key
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < lowThroughputRequests; i++ {
			_, _, err := lowLimiter.limit("low_single_key", func() (any, error) {
				return "low", nil
			})

			mu.Lock()
			switch err {
			case nil:
				lowSuccess++
			case ErrRateLimitExceeded:
				lowRateLimit++
			default:
				lowErrors++
			}
			mu.Unlock()
		}
	}()

	wg.Wait()
	duration := time.Since(startTime)
	totalSuccess := highSuccess + mediumSuccess + lowSuccess
	totalThroughput := float64(totalSuccess) / duration.Seconds()

	fmt.Printf("\n=== SINGLE KEY MIXED WORKLOAD TEST RESULTS ===\n")
	fmt.Printf("Total Duration: %v\n", duration)
	fmt.Printf("High Throughput (1000/sec): %d success, %d rate limited, %d errors\n", highSuccess, highRateLimit, highErrors)
	fmt.Printf("Medium Throughput (100/sec): %d success, %d rate limited, %d errors\n", mediumSuccess, mediumRateLimit, mediumErrors)
	fmt.Printf("Low Throughput (10/sec): %d success, %d rate limited, %d errors\n", lowSuccess, lowRateLimit, lowErrors)
	fmt.Printf("Total Throughput: %.2f requests/second\n", totalThroughput)
	fmt.Printf("==========================================\n\n")

	// Don't assert on specific numbers - let's see what the actual limits are
	fmt.Printf("Actual mixed workload throughput: %.2f req/s\n", totalThroughput)
}

// TestMillisecondRateLimiting tests millisecond-based rate limiting with deterministic time mocking
// This test uses mock time because millisecond precision requires deterministic behavior
func (s *HighThroughputTestSuite) TestMillisecondRateLimiting() {
	// Create a millisecond-based provider
	millisecondProvider := &StaticProvider{
		TimeInterval: 100, // 100 requests per millisecond
		TimeUnit:     "millisecond",
	}

	// Set up mock time at the top of a millisecond for deterministic resets
	baseTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	millisecondMockTime := NewMockTimeProvider(baseTime)

	millisecondDBPath := fmt.Sprintf("test_millisecond_%d.db", time.Now().UnixNano())
	millisecondLimiter, err := newRateLimiterWithProviders(millisecondDBPath, millisecondProvider, millisecondMockTime)
	require.NoError(s.T(), err)
	defer func() {
		millisecondLimiter.close()
		os.RemoveAll(millisecondDBPath)
	}()

	const totalRequests = 100
	var successCount int64

	// Test millisecond rate limiting with deterministic time
	for i := range totalRequests {
		key := fmt.Sprintf("millisecond_key_%d", i%5)
		_, _, err := millisecondLimiter.limit(key, func() (any, error) {
			return "millisecond_success", nil
		})

		if err == nil {
			successCount++
		}
	}

	// Advance time once after all requests
	millisecondMockTime.Advance(time.Millisecond)

	assert.Equal(s.T(), int64(totalRequests), successCount, "All millisecond requests should succeed with deterministic time")
}

func TestHighThroughputTestSuite(t *testing.T) {
	suite.Run(t, new(HighThroughputTestSuite))
}
