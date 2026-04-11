package cost

import (
	"testing"

	"claude-codex/internal/public/types"
	"github.com/stretchr/testify/assert"
)

func TestTracker_AddUsage(t *testing.T) {
	tracker := NewTracker()

	usage := &types.Usage{
		InputTokens:              1000,
		OutputTokens:             500,
		CacheCreationInputTokens: 100,
		CacheReadInputTokens:     200,
	}

	tracker.AddUsage("claude-3-5-sonnet-20241022", usage, 0.015, 200000, 8192)

	assert.Equal(t, 0.015, tracker.GetTotalCostUSD())
	assert.Equal(t, 1000, tracker.GetTotalInputTokens())
	assert.Equal(t, 500, tracker.GetTotalOutputTokens())
	assert.Equal(t, 100, tracker.GetTotalCacheCreationInputTokens())
	assert.Equal(t, 200, tracker.GetTotalCacheReadInputTokens())

	modelUsage := tracker.GetUsageForModel("claude-3-5-sonnet-20241022")
	assert.NotNil(t, modelUsage)
	assert.Equal(t, 1000, modelUsage.InputTokens)
	assert.Equal(t, 500, modelUsage.OutputTokens)
	assert.Equal(t, 0.015, modelUsage.CostUSD)
}

func TestTracker_MultipleModels(t *testing.T) {
	tracker := NewTracker()

	usage1 := &types.Usage{InputTokens: 1000, OutputTokens: 500}
	usage2 := &types.Usage{InputTokens: 2000, OutputTokens: 1000}

	tracker.AddUsage("model-1", usage1, 0.01, 100000, 4096)
	tracker.AddUsage("model-2", usage2, 0.02, 200000, 8192)

	assert.Equal(t, 0.03, tracker.GetTotalCostUSD())
	assert.Equal(t, 3000, tracker.GetTotalInputTokens())
	assert.Equal(t, 1500, tracker.GetTotalOutputTokens())

	allUsage := tracker.GetModelUsage()
	assert.Len(t, allUsage, 2)
	assert.Equal(t, 1000, allUsage["model-1"].InputTokens)
	assert.Equal(t, 2000, allUsage["model-2"].InputTokens)
}

func TestTracker_AddDurations(t *testing.T) {
	tracker := NewTracker()

	tracker.AddAPIDuration(1000, true)
	tracker.AddAPIDuration(500, false)
	tracker.AddToolDuration(300)

	assert.Equal(t, int64(1500), tracker.GetTotalAPIDuration())
	assert.Equal(t, int64(500), tracker.GetTotalAPIDurationWithoutRetries())
	assert.Equal(t, int64(300), tracker.GetTotalToolDuration())
}

func TestTracker_AddLinesChanged(t *testing.T) {
	tracker := NewTracker()

	tracker.AddLinesChanged(100, 50)
	tracker.AddLinesChanged(200, 75)

	assert.Equal(t, 300, tracker.GetTotalLinesAdded())
	assert.Equal(t, 125, tracker.GetTotalLinesRemoved())
}

func TestTracker_Reset(t *testing.T) {
	tracker := NewTracker()

	usage := &types.Usage{InputTokens: 1000, OutputTokens: 500}
	tracker.AddUsage("model-1", usage, 0.01, 100000, 4096)
	tracker.AddLinesChanged(100, 50)
	tracker.SetHasUnknownModelCost(true)

	tracker.Reset()

	assert.Equal(t, 0.0, tracker.GetTotalCostUSD())
	assert.Equal(t, 0, tracker.GetTotalInputTokens())
	assert.Equal(t, 0, tracker.GetTotalOutputTokens())
	assert.Equal(t, 0, tracker.GetTotalLinesAdded())
	assert.Equal(t, 0, tracker.GetTotalLinesRemoved())
	assert.False(t, tracker.HasUnknownModelCost())
	assert.Empty(t, tracker.GetModelUsage())
}

func TestTracker_SnapshotAndRestore(t *testing.T) {
	tracker := NewTracker()

	usage := &types.Usage{
		InputTokens:              1000,
		OutputTokens:             500,
		CacheCreationInputTokens: 100,
		CacheReadInputTokens:     200,
	}
	tracker.AddUsage("model-1", usage, 0.01, 100000, 4096)
	tracker.AddLinesChanged(100, 50)
	tracker.AddAPIDuration(1000, true)
	tracker.SetHasUnknownModelCost(true)

	snapshot := tracker.GetSnapshot()

	// Create new tracker and restore
	newTracker := NewTracker()
	newTracker.RestoreFromSnapshot(snapshot)

	assert.Equal(t, tracker.GetTotalCostUSD(), newTracker.GetTotalCostUSD())
	assert.Equal(t, tracker.GetTotalInputTokens(), newTracker.GetTotalInputTokens())
	assert.Equal(t, tracker.GetTotalOutputTokens(), newTracker.GetTotalOutputTokens())
	assert.Equal(t, tracker.GetTotalLinesAdded(), newTracker.GetTotalLinesAdded())
	assert.Equal(t, tracker.GetTotalLinesRemoved(), newTracker.GetTotalLinesRemoved())
	assert.Equal(t, tracker.GetTotalAPIDuration(), newTracker.GetTotalAPIDuration())
	assert.Equal(t, tracker.HasUnknownModelCost(), newTracker.HasUnknownModelCost())

	originalUsage := tracker.GetUsageForModel("model-1")
	restoredUsage := newTracker.GetUsageForModel("model-1")
	assert.Equal(t, originalUsage.InputTokens, restoredUsage.InputTokens)
	assert.Equal(t, originalUsage.OutputTokens, restoredUsage.OutputTokens)
	assert.Equal(t, originalUsage.CostUSD, restoredUsage.CostUSD)
}

func TestTracker_ConcurrentAccess(t *testing.T) {
	tracker := NewTracker()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			usage := &types.Usage{InputTokens: 100, OutputTokens: 50}
			tracker.AddUsage("model-1", usage, 0.001, 100000, 4096)
			tracker.AddLinesChanged(10, 5)
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 1000, tracker.GetTotalInputTokens())
	assert.Equal(t, 500, tracker.GetTotalOutputTokens())
	assert.Equal(t, 100, tracker.GetTotalLinesAdded())
	assert.Equal(t, 50, tracker.GetTotalLinesRemoved())
}
