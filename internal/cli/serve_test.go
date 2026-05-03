package cli

import (
	"testing"

	"github.com/cgould/dtree/internal/server"
)

// TestValidateTrustAddr exercises the validateTrustAddr helper.
func TestValidateTrustAddr(t *testing.T) {
	cases := []struct {
		name    string
		trust   server.Trust
		addr    string
		wantErr bool
	}{
		{
			name:    "localhost trust on loopback addr is ok",
			trust:   server.TrustLocalhostOnly,
			addr:    "127.0.0.1:8080",
			wantErr: false,
		},
		{
			name:    "localhost trust on 0.0.0.0 is rejected",
			trust:   server.TrustLocalhostOnly,
			addr:    "0.0.0.0:8080",
			wantErr: true,
		},
		{
			name:    "localhost trust on public IP is rejected",
			trust:   server.TrustLocalhostOnly,
			addr:    "203.0.113.1:8080",
			wantErr: true,
		},
		{
			name:    "token trust on public IP is ok",
			trust:   server.TrustToken,
			addr:    "0.0.0.0:8080",
			wantErr: false,
		},
		{
			name:    "token trust on loopback is also ok",
			trust:   server.TrustToken,
			addr:    "127.0.0.1:8080",
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTrustAddr(tc.trust, tc.addr)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateTrustAddr(%v, %q) error = %v, wantErr = %v", tc.trust, tc.addr, err, tc.wantErr)
			}
		})
	}
}
