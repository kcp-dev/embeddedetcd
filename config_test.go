/*
Copyright 2026 The KCP Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package embeddedetcd

import (
	"os"
	"testing"

	"go.etcd.io/etcd/server/v3/storage/wal"

	"github.com/kcp-dev/embeddedetcd/options"
)

func TestUnsafeE2EHackDisableEtcdFsync(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name                    string
		envValue                string
		walSizeBytesOption      int64
		expectUnsafeNoFsync     bool
		expectMaxWalFiles       uint
		expectMaxSnapFiles      uint
		expectWalSegmentSize    int64
	}{
		{
			name:                 "env var not set",
			envValue:             "",
			walSizeBytesOption:   0,
			expectUnsafeNoFsync:  false,
			expectMaxWalFiles:    5, // etcd default
			expectMaxSnapFiles:   5, // etcd default
			expectWalSegmentSize: 64 * 1024 * 1024, // 64MB default
		},
		{
			name:                 "env var set to false",
			envValue:             "false",
			walSizeBytesOption:   0,
			expectUnsafeNoFsync:  false,
			expectMaxWalFiles:    5,
			expectMaxSnapFiles:   5,
			expectWalSegmentSize: 64 * 1024 * 1024,
		},
		{
			name:                 "env var set to true",
			envValue:             "true",
			walSizeBytesOption:   0,
			expectUnsafeNoFsync:  true,
			expectMaxWalFiles:    1,
			expectMaxSnapFiles:   1,
			expectWalSegmentSize: 1 << 20, // 1MB
		},
		{
			name:                 "env var set to true with custom WAL size",
			envValue:             "true",
			walSizeBytesOption:   2 << 20, // 2MB
			expectUnsafeNoFsync:  true,
			expectMaxWalFiles:    1,
			expectMaxSnapFiles:   1,
			expectWalSegmentSize: 2 << 20, // Should keep custom size
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the environment variable
			if tt.envValue != "" {
				os.Setenv("UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC", tt.envValue)
			} else {
				os.Unsetenv("UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC")
			}
			defer os.Unsetenv("UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC")

			// Reset WAL segment size to default before each test
			wal.SegmentSizeBytes = 64 * 1024 * 1024

			// Create options
			opts := options.NewOptions(tmpDir)
			opts.WalSizeBytes = tt.walSizeBytesOption
			completedOpts := opts.Complete(nil)

			// Create config
			cfg, err := NewConfig(completedOpts, false)
			if err != nil {
				t.Fatalf("NewConfig failed: %v", err)
			}

			// Verify settings
			if cfg.UnsafeNoFsync != tt.expectUnsafeNoFsync {
				t.Errorf("UnsafeNoFsync: got %v, want %v", cfg.UnsafeNoFsync, tt.expectUnsafeNoFsync)
			}

			if cfg.MaxWalFiles != tt.expectMaxWalFiles {
				t.Errorf("MaxWalFiles: got %v, want %v", cfg.MaxWalFiles, tt.expectMaxWalFiles)
			}

			if cfg.MaxSnapFiles != tt.expectMaxSnapFiles {
				t.Errorf("MaxSnapFiles: got %v, want %v", cfg.MaxSnapFiles, tt.expectMaxSnapFiles)
			}

			if wal.SegmentSizeBytes != tt.expectWalSegmentSize {
				t.Errorf("wal.SegmentSizeBytes: got %v, want %v", wal.SegmentSizeBytes, tt.expectWalSegmentSize)
			}
		})
	}
}
