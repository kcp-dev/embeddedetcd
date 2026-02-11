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

// Benchmarks to measure the performance impact of UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC optimizations.
//
// Run benchmarks with:
//
//	go test -bench=. -benchmem -benchtime=10s
//
// These benchmarks compare three modes:
//  1. BenchmarkNormalMode - Default disk-based operation with fsync enabled
//  2. BenchmarkUnsafeModeOld - UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC=true WITHOUT optimizations (MaxWalFiles/MaxSnapFiles not minimized)
//  3. BenchmarkUnsafeModeOptimized - UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC=true WITH WAL/snapshot minimizations (actual new behavior)

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"go.etcd.io/etcd/server/v3/storage/wal"
)

// BenchmarkNormalMode benchmarks the default disk-based operation with fsync enabled.
func BenchmarkNormalMode(b *testing.B) {
	runBenchmark(b, benchmarkConfig{
		unsafeNoFsync: false,
		setMaxFiles:   false,
		walSizeBytes:  0,
	})
}

// BenchmarkUnsafeModeOld benchmarks UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC=true without the new optimizations.
// This simulates the old behavior by NOT setting MaxWalFiles/MaxSnapFiles.
func BenchmarkUnsafeModeOld(b *testing.B) {
	runBenchmark(b, benchmarkConfig{
		unsafeNoFsync: true,
		setMaxFiles:   false,
		walSizeBytes:  0,
	})
}

// BenchmarkUnsafeModeOptimized benchmarks UNSAFE_E2E_HACK_DISABLE_ETCD_FSYNC=true with the new optimizations.
// This is the actual new behavior with WAL/snapshot minimizations.
func BenchmarkUnsafeModeOptimized(b *testing.B) {
	runBenchmark(b, benchmarkConfig{
		unsafeNoFsync: true,
		setMaxFiles:   true,
		walSizeBytes:  1 << 20, // 1MB
	})
}

type benchmarkConfig struct {
	unsafeNoFsync bool
	setMaxFiles   bool
	walSizeBytes  int64
}

// freePort returns a free TCP port on localhost.
func freePort(b *testing.B) string {
	b.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		b.Fatalf("Failed to find free port: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

func runBenchmark(b *testing.B, cfg benchmarkConfig) {
	tmpDir := b.TempDir()

	// Save and restore original WAL segment size
	originalWalSize := wal.SegmentSizeBytes
	defer func() {
		wal.SegmentSizeBytes = originalWalSize
	}()

	// Configure WAL size
	if cfg.walSizeBytes > 0 {
		wal.SegmentSizeBytes = cfg.walSizeBytes
	} else {
		wal.SegmentSizeBytes = 64 * 1024 * 1024 // 64MB default
	}

	// Allocate free ports so benchmarks don't collide
	clientPort := freePort(b)
	peerPort := freePort(b)

	etcdCfg := embed.NewConfig()
	etcdCfg.Logger = "zap"
	etcdCfg.LogLevel = "fatal"
	etcdCfg.Dir = filepath.Join(tmpDir, "etcd-data")
	etcdCfg.ListenClientUrls = []url.URL{{Scheme: "http", Host: "localhost:" + clientPort}}
	etcdCfg.AdvertiseClientUrls = []url.URL{{Scheme: "http", Host: "localhost:" + clientPort}}
	etcdCfg.ListenPeerUrls = []url.URL{{Scheme: "http", Host: "localhost:" + peerPort}}
	etcdCfg.AdvertisePeerUrls = []url.URL{{Scheme: "http", Host: "localhost:" + peerPort}}
	etcdCfg.InitialCluster = etcdCfg.InitialClusterFromName(etcdCfg.Name)
	etcdCfg.UnsafeNoFsync = cfg.unsafeNoFsync

	if cfg.setMaxFiles {
		etcdCfg.MaxWalFiles = 1
		etcdCfg.MaxSnapFiles = 1
	}

	e, err := embed.StartEtcd(etcdCfg)
	if err != nil {
		b.Fatalf("Failed to start etcd: %v", err)
	}
	defer e.Close()

	// Wait for server to be ready
	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(60 * time.Second):
		b.Fatal("Server took too long to start")
	case err := <-e.Err():
		b.Fatalf("Server error: %v", err)
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"http://localhost:" + clientPort},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		key := fmt.Sprintf("/benchmark/key-%d", i)
		value := fmt.Sprintf("value-%d", i)
		_, err := client.Put(ctx, key, value)
		cancel()
		if err != nil {
			b.Fatalf("Failed to put key: %v", err)
		}

		// Read the key back
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		resp, err := client.Get(ctx, key)
		cancel()
		if err != nil {
			b.Fatalf("Failed to get key: %v", err)
		}
		if len(resp.Kvs) != 1 {
			b.Fatalf("Expected 1 key, got %d", len(resp.Kvs))
		}
	}

	b.StopTimer()

	// Measure disk usage and file counts
	diskUsedMB, walFiles, snapFiles := measureDiskMetrics(etcdCfg.Dir)
	b.ReportMetric(diskUsedMB, "diskUsedMB")
	b.ReportMetric(float64(walFiles), "walFileCount")
	b.ReportMetric(float64(snapFiles), "snapFileCount")
}

// measureDiskMetrics calculates disk usage and counts WAL and snapshot files.
func measureDiskMetrics(dir string) (diskUsedMB float64, walFiles int, snapFiles int) {
	var totalBytes int64

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error { //nolint:errcheck
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			totalBytes += info.Size()

			if filepath.Dir(path) == filepath.Join(dir, "member", "wal") && filepath.Ext(path) == ".wal" {
				walFiles++
			}
			if filepath.Dir(path) == filepath.Join(dir, "member", "snap") && filepath.Ext(path) == ".snap" {
				snapFiles++
			}
		}
		return nil
	})

	diskUsedMB = float64(totalBytes) / (1024 * 1024)
	return
}
