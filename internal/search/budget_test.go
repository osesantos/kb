package search

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/osesantos/kb/internal/vault"
)

// TestBudgets reports load time and query p95 on the vault in KB_VAULT against the HLD budgets (150 ms, 30 ms).
func TestBudgets(t *testing.T) {
	root := os.Getenv("KB_VAULT")
	if root == "" {
		t.Skip("set KB_VAULT to measure")
	}
	loads := []time.Duration{}
	var v *vault.Vault
	for range 5 {
		s := time.Now()
		v = vault.Load(root)
		loads = append(loads, time.Since(s))
	}
	slices.Sort(loads)
	queries := []string{"qdrant", "obsidian git commit", "external library latency loki", "dyslexia", "overseer swarm", "tenant isolation", "workflow terminate", "rdinc triage", "cargo install", "daily note", "the"}
	times := []time.Duration{}
	for range 20 {
		for _, q := range queries {
			s := time.Now()
			Lexical(v, q, 20)
			times = append(times, time.Since(s))
		}
	}
	slices.Sort(times)
	p95 := times[len(times)*95/100]
	t.Logf("notes=%d load median=%v min=%v | %d queries: median=%v p95=%v max=%v", len(v.Notes), loads[2], loads[0], len(times), times[len(times)/2], p95, times[len(times)-1])
	if loads[2] > 150*time.Millisecond || p95 > 30*time.Millisecond {
		t.Errorf("over budget: load %v (150ms) p95 %v (30ms)", loads[2], p95)
	}
}
