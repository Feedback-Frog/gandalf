package gandalf

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataProvider(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		fetcherReturn struct {
			rateLimit     int
			rateLimitUnit string
			err           error
		}
		wantData RateLimitData
		wantErr  bool
	}{
		{
			name: "successful fetch",
			key:  "test-key-1",
			fetcherReturn: struct {
				rateLimit     int
				rateLimitUnit string
				err           error
			}{
				rateLimit:     100,
				rateLimitUnit: "second",
				err:           nil,
			},
			wantData: RateLimitData{
				RateLimit:     100,
				RateLimitUnit: "second",
			},
			wantErr: false,
		},
		{
			name: "fetcher returns error",
			key:  "test-key-2",
			fetcherReturn: struct {
				rateLimit     int
				rateLimitUnit string
				err           error
			}{
				rateLimit:     0,
				rateLimitUnit: "",
				err:           fmt.Errorf("database error"),
			},
			wantData: RateLimitData{},
			wantErr:  true,
		},
		{
			name: "zero rate limit",
			key:  "test-key-3",
			fetcherReturn: struct {
				rateLimit     int
				rateLimitUnit string
				err           error
			}{
				rateLimit:     0,
				rateLimitUnit: "minute",
				err:           nil,
			},
			wantData: RateLimitData{
				RateLimit:     0,
				RateLimitUnit: "minute",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fetcher that returns the test case values
			fetcher := func(key string) (int, string, error) {
				assert.Equal(t, tt.key, key, "fetcher received unexpected key")
				return tt.fetcherReturn.rateLimit, tt.fetcherReturn.rateLimitUnit, tt.fetcherReturn.err
			}

			provider := NewDataProvider(fetcher)
			gotData, err := provider.GetRateLimitData(tt.key)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantData, gotData)
			}
		})
	}
}

func TestStaticProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider StaticProvider
		key      string
		want     RateLimitData
		wantErr  bool
	}{
		{
			name: "valid rate limit",
			provider: StaticProvider{
				TimeInterval: 100,
				TimeUnit:     "second",
			},
			key: "any-key",
			want: RateLimitData{
				RateLimit:     100,
				RateLimitUnit: "second",
			},
			wantErr: false,
		},
		{
			name: "zero rate limit",
			provider: StaticProvider{
				TimeInterval: 0,
				TimeUnit:     "minute",
			},
			key: "any-key",
			want: RateLimitData{
				RateLimit:     0,
				RateLimitUnit: "minute",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.provider.GetRateLimitData(tt.key)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
