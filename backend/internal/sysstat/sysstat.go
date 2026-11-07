// Package sysstat samples host and process resource usage for node heartbeats.
// Network rates come from the difference between two consecutive samples.
package sysstat

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// Sample is one point-in-time measurement. Values that could not be read are zero.
type Sample struct {
	CPUPercent float64 `json:"cpu_percent"`
	CPUCores   int     `json:"cpu_cores"`
	ProcCPU    float64 `json:"proc_cpu_percent"`
	MemUsed    uint64  `json:"mem_used"`
	MemTotal   uint64  `json:"mem_total"`
	ProcRSS    uint64  `json:"proc_rss"`
	NetRxBps   uint64  `json:"net_rx_bps"`
	NetTxBps   uint64  `json:"net_tx_bps"`
	NetRxTotal uint64  `json:"net_rx_total"`
	NetTxTotal uint64  `json:"net_tx_total"`
}

// Sampler keeps the previous network counters so it can report rates.
type Sampler struct {
	mu     sync.Mutex
	proc   *process.Process
	prevAt time.Time
	prevRx uint64
	prevTx uint64
}

// New returns a sampler for the current process.
func New() *Sampler {
	s := &Sampler{}
	if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
		s.proc = p
	}
	return s
}

// Sample measures CPU since the previous call, memory now and network rates
// since the previous call. The first call reports zero network rates.
func (s *Sampler) Sample(ctx context.Context) Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Sample
	if values, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(values) == 1 {
		out.CPUPercent = values[0]
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		out.CPUCores = n
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		out.MemUsed, out.MemTotal = vm.Used, vm.Total
	}
	if s.proc != nil {
		if info, err := s.proc.MemoryInfoWithContext(ctx); err == nil {
			out.ProcRSS = info.RSS
		}
		if pct, err := s.proc.PercentWithContext(ctx, 0); err == nil {
			out.ProcCPU = pct
		}
	}
	if counters, err := net.IOCountersWithContext(ctx, true); err == nil {
		var rx, tx uint64
		for _, c := range counters {
			if isLoopback(c.Name) {
				continue
			}
			rx += c.BytesRecv
			tx += c.BytesSent
		}
		now := time.Now()
		out.NetRxTotal, out.NetTxTotal = rx, tx
		if !s.prevAt.IsZero() {
			if seconds := now.Sub(s.prevAt).Seconds(); seconds > 0 {
				if rx >= s.prevRx {
					out.NetRxBps = uint64(float64(rx-s.prevRx) / seconds)
				}
				if tx >= s.prevTx {
					out.NetTxBps = uint64(float64(tx-s.prevTx) / seconds)
				}
			}
		}
		s.prevAt, s.prevRx, s.prevTx = now, rx, tx
	}
	return out
}

func isLoopback(name string) bool {
	return name == "lo" || strings.HasPrefix(name, "lo0") || strings.HasPrefix(name, "Loopback")
}
