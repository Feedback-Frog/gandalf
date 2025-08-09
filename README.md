<p align="center">
  <img src="./gandalf-vs-ferris.png" alt="The Go Gopher as Gandalf, facing down a fiery Rust-colored crab monster." height="450"/>
</p>

# GANDALF: *G*olang *A*pplication *N*etwork *D*efense *A*nd *L*imit *F*ilter

[![Go Reference](https://pkg.go.dev/badge/github.com/feedback-frog/gandalf.svg)](https://pkg.go.dev/github.com/feedback-frog/gandalf)
[![Go Report Card](https://goreportcard.com/badge/github.com/feedback-frog/gandalf)](https://goreportcard.com/report/github.com/feedback-frog/gandalf)
[![Coverage Status](https://coveralls.io/repos/github/Feedback-Frog/gandalf/badge.svg?branch=main)](https://coveralls.io/github/Feedback-Frog/gandalf?branch=main)
[![CI](https://github.com/Feedback-Frog/gandalf/actions/workflows/ci.yml/badge.svg)](https://github.com/Feedback-Frog/gandalf/actions/workflows/ci.yml)

Used reliably in production since 2022 at <https://feedbackfrog.io>

## With GANDALF, You Shall Not Pass… the Rate Limit

In the deepest forges of application development, where the fires of high traffic burn brightest, GANDALF stands as a formidable guardian. This rate limiting library for Go applications, it provides a simple yet powerful API to shield your services from overwhelming requests. Like a wizard's staff, it offers protection and control, ensuring that only the worthy may pass through your system's gates. A true upgrade to your arsenal.

## Features

- **Ultra-fast performance**: Up to 38,000+ requests/second for sequential operations
- **Reliable concurrent throughput**: 3,600+ requests/second
- **Persistent storage** using BadgerDB (significantly faster than BoltDB equivalents for writes)
- **Support for different time units** (millisecond, second, minute, hour, day, month)
- **Customizable rate limits** per API key
- **Thread-safe operations** with intelligent retry mechanisms
- **Easy integration** with existing applications
- **Advanced transaction conflict resolution** with exponential backoff and jitter

## Performance Highlights

Our comprehensive benchmarks demonstrate exceptional performance with BadgerDB:

- **Sequential Operations**: 38,327 requests/second
- **Concurrent Operations**: 3,604 requests/second
- **Mixed Workload**: 38,988 requests/second (multiple rate limit tiers)
- **Millisecond Precision**: 100+ requests per millisecond with deterministic resets
- **Persistence**: BadgerDB with 10x+ write performance improvement over BoltDB

## Installation

```bash
go get github.com/feedback-frog/gandalf
```

## Basic Usage

```go
package main

import (
    "fmt"
    "log"

    "github.com/feedback-frog/gandalf"
)

func main() {
    // Create a static provider (1000 requests per second for all keys)
    provider := &gandalf.StaticProvider{
        TimeInterval: 1000,
        TimeUnit:     "second",
    }

    // Create a rate limiter
    limiter, err := gandalf.NewRateLimiterWithProvider("rate_limits.db", provider)
    if err != nil {
        log.Fatal(err)
    }
    defer limiter.Close()

    // Use rate limiting
    result, funcErr, rateLimitErr := limiter.Limit("user-123", func() (any, error) {
        return "API call successful", nil
    })

    if rateLimitErr != nil {
        if rateLimitErr == gandalf.ErrRateLimitExceeded {
            fmt.Println("Rate limit exceeded!")
            return
        }
        log.Fatal(rateLimitErr)
    }

    if funcErr != nil {
        log.Fatal(funcErr)
    }

    fmt.Printf("Result: %v\n", result)
}
```

## Gin (or any mux) HTTP Middleware

Here's how to integrate GANDALF as middleware:

```go
package main

import (
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/feedback-frog/gandalf"
)

// RateLimitMiddleware creates a Gin middleware that uses GANDALF for rate limiting
func RateLimitMiddleware(limiter *gandalf.RateLimiter) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Extract client identifier (IP address, API key, user ID, service ID, etc.)
        clientID := getClientID(c)

        // Use GANDALF to check rate limit
        _, funcErr, rateLimitErr := limiter.Limit(clientID, func() (any, error) {
            // This function will only execute if rate limit allows
            // We return nil since we just want to check/consume the rate limit
            return nil, nil
        })

        if rateLimitErr != nil {
            if rateLimitErr == gandalf.ErrRateLimitExceeded {
                c.JSON(http.StatusTooManyRequests, gin.H{
                    "error": "Rate limit exceeded",
                    "message": "Too many requests, please try again later",
                })
                c.Abort()
                return
            }
            // Handle other rate limiting errors
            c.JSON(http.StatusInternalServerError, gin.H{
                "error": "Rate limiting service error",
            })
            c.Abort()
            return
        }

        if funcErr != nil {
            c.JSON(http.StatusInternalServerError, gin.H{
                "error": "Rate limiting function error",
            })
            c.Abort()
            return
        }

        // Rate limit check passed, continue to next middleware/handler
        c.Next()
    }
}

func main() {
    // Create a static provider (100 requests per minute for all clients)
    provider := &gandalf.StaticProvider{
        TimeInterval: 100,
        TimeUnit:     "minute",
    }

    // Create rate limiter with BadgerDB
    limiter, err := gandalf.NewRateLimiterWithProvider("api_rate_limits.db", provider)
    if err != nil {
        panic(err)
    }
    defer limiter.Close()

    // Create Gin router
    r := gin.Default()

    // Apply rate limiting middleware globally
    r.Use(RateLimitMiddleware(limiter))

    // Define routes
    r.GET("/api/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{
            "status": "healthy",
            "message": "API is running with GANDALF rate limiting",
        })
    })

    r.POST("/api/data", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{
            "message": "Data processed successfully",
            "client_id": getClientID(c),
        })
    })

    // Start server
    r.Run(":8080")
}
```

### Per-Route Rate Limiting

For different rate limits on different endpoints:

```go

func main() {
    // Create different providers for different endpoints
    strictProvider := &gandalf.StaticProvider{
        TimeInterval: 10,
        TimeUnit:     "minute",
    }

    relaxedProvider := &gandalf.StaticProvider{
        TimeInterval: 1000,
        TimeUnit:     "hour",
    }

    strictLimiter, _ := gandalf.NewRateLimiterWithProvider("strict_limits.db", strictProvider)
    relaxedLimiter, _ := gandalf.NewRateLimiterWithProvider("relaxed_limits.db", relaxedProvider)
    defer strictLimiter.Close()
    defer relaxedLimiter.Close()

    r := gin.Default()

    // Strict rate limiting for sensitive endpoints
    sensitive := r.Group("/api/admin")
    sensitive.Use(RateLimitMiddleware(strictLimiter)) // RateLimitMiddleware defined earlier in the README
    {
        sensitive.GET("/users", func(c *gin.Context) {
            c.JSON(http.StatusOK, gin.H{"message": "Admin endpoint"})
        })
    }

    // Relaxed rate limiting for public endpoints
    public := r.Group("/api/public")
    public.Use(RateLimitMiddleware(relaxedLimiter))
    {
        public.GET("/data", func(c *gin.Context) {
            c.JSON(http.StatusOK, gin.H{"data": "public data"})
        })
    }

    r.Run(":8080")
```

## Database-Backed Rate Limiting

```go
package main

import (
    "fmt"
    "log"

    "github.com/feedback-frog/gandalf"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
)

// ServiceLimits represents your application's service limits model
type ServiceLimits struct {
    ID            string `gorm:"primaryKey"`
    Key           string `gorm:"uniqueIndex"`
    ProjectID     string
    RateLimit     int
    RateLimitUnit string
}

func main() {
    // using GORM with SQLite as a simple example to copy/paste.
    // Gandalf is data store agnostic
    db, err := gorm.Open(sqlite.Open("api_keys.db"), &gorm.Config{})
    if err != nil {
        log.Fatal(err)
    }

    // Auto-migrate the API key table
    db.AutoMigrate(&ServiceLimits{})

    // Create some API keys
    db.Create(&ServiceLimits{
        ID:            "api-key-1",
        Key:           "sk-test-123",
        ProjectID:     "project-1",
        RateLimit:     1000, // High throughput: 1000 requests per day
        RateLimitUnit: "day",
    })

    // Create a rate limit fetcher function that gets data from your database
    rateLimitFetcher := func(key string) (int, string, error) {
        var apiKey ServiceLimits
        result := db.Where("key = ?", key).First(&apiKey)
        if result.Error != nil {
            if result.Error == gorm.ErrRecordNotFound {
                return 0, "", fmt.Errorf("unknown key")
            }
            return 0, "", result.Error
        }
        return apiKey.RateLimit, apiKey.RateLimitUnit, nil
    }

    // Create provider with your custom fetcher
    provider := gandalf.NewDataProvider(rateLimitFetcher)

    // Create rate limiter with BadgerDB for optimal performance
    limiter, err := gandalf.NewRateLimiterWithProvider("rate_limits.db", provider)
    if err != nil {
        log.Fatal(err)
    }
    defer limiter.Close()

    // Use with actual Service key
    result, funcErr, rateLimitErr := limiter.Limit("sk-test-123", func() (any, error) {
        return "Database-backed rate limiting with BadgerDB!", nil
    })

    if rateLimitErr == gandalf.ErrRateLimitExceeded {
        log.Println("Rate limit exceeded!")
        return
    }

    if funcErr != nil {
        log.Fatal(funcErr)
    }

    fmt.Printf("Result: %v\n", result)
}
```

## API Reference

### RateLimiter

The main type that provides rate limiting functionality with BadgerDB backend.

#### Methods

- `NewRateLimiter(dbPath string) (*RateLimiter, error)`
  - Creates a new rate limiter with the specified BadgerDB path
  - Returns a RateLimiter instance and any error that occurred

- `NewRateLimiterWithProvider(dbPath string, provider RateLimitDataProvider) (*RateLimiter, error)`
  - Creates a new rate limiter with a custom data provider
  - Returns a RateLimiter instance and any error that occurred

- `Limit(keyId string, fn func() (any, error)) (any, error, error)`
  - Executes a function with rate limiting
  - Returns (functionResult, functionError, rateLimitError)
  - If rate limit is exceeded, returns (nil, nil, ErrRateLimitExceeded)
  - If rate limit allows execution, consumes one request and returns function results
  - **Includes intelligent retry logic** with exponential backoff and jitter for BadgerDB transaction conflicts

- `Peek(keyId string) (requestsLeft int, limitUnit string, err error)`
  - Returns the current limit and limit unit for a given key without consuming a request
  - Returns the number of requests left, the limit unit, and any error that occurred

- `Purge(keyId string) error`
  - Resets the current limits for a key, or optionally all keys if keyId is empty
  - Returns any error that occurred

- `Close() error`
  - Closes the underlying BadgerDB database
  - Returns any error that occurred

### Providers

#### StaticProvider

A provider that returns a static rate limit for all keys.

```go
provider := &gandalf.StaticProvider{
    TimeInterval: 1000, // 1000 requests
    TimeUnit:     "second", // per second
}
```

#### DataProvider

A provider that gets rate limit data from any source using a fetcher function.

```go
// Create a rate limit fetcher function
rateLimitFetcher := func(key string) (int, string, error) {
    // Get rate limit data from your source (database, API, etc.)
    // Return (rateLimit, rateLimitUnit, error)
    return 1000, "second", nil
}

// Create provider with your custom fetcher
provider := gandalf.NewDataProvider(rateLimitFetcher)
```

The fetcher function should:

- Return the rate limit (number of requests allowed)
- Return the rate limit unit ("millisecond", "second", "minute", "hour", "day", "month")
- Return an error if the key is not found or any other error occurs

### Rate Limit Units

The rate limiter supports the following time units:

- `millisecond`: Resets every millisecond (up to 100+ requests/ms)
- `second`: Resets every second (up to 38,000+ requests/sec)
- `minute`: Resets every minute
- `hour`: Resets every hour
- `day`: Resets every day
- `month`: Resets every month

## Performance Characteristics

### Throughput Benchmarks

Our comprehensive single-key testing shows real-world performance:

| Scenario | Throughput | Success Rate | Description |
|----------|------------|--------------|-------------|
| Sequential Operations | 38,327 req/sec | 100% | Single-threaded, single key |
| Concurrent Operations | 3,604 req/sec | 99.8% | Single key, concurrent requests |
| Mixed Workload | 38,988 req/sec | 100% | Multiple rate limit tiers, distributed keys |
| Millisecond Precision | 100+ req/ms | 100% | Deterministic millisecond-based rate limiting |

### Retry Logic Performance

Gandalf's intelligent retry mechanism dramatically improves reliability, much like how the Grey Wizard persevered against the Balrog and returned stronger as Gandalf the White:

- **10 retry attempts** with exponential backoff (0.5ms → 256ms)
- **Jitter (±25%)** to prevent thundering herd problems
- **Automatic transaction conflict resolution** for BadgerDB

### Real-World Usage Patterns

For optimal performance:

1. **Sequential workloads**: Achieve 38k+ req/sec with 100% success rate
2. **Concurrent workloads**: Use multiple keys to distribute load
3. **High-throughput scenarios**: Leverage BadgerDB's LSM tree structure
4. **Production deployments**: Monitor transaction conflict rates

## Error Handling

The rate limiter provides clear error handling for various scenarios:

```go
result, funcErr, rateLimitErr := limiter.Limit("user-123", func() (any, error) {
    return "API call", nil
})

if rateLimitErr == gandalf.ErrRateLimitExceeded {
    fmt.Println("Rate limit exceeded!")
    return
}

if funcErr != nil {
    log.Printf("Function error: %v\n", funcErr)
    return
}

fmt.Printf("Result: %v\n", result)
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License.
