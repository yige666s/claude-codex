package tool

import (
	"sync"
	"testing"
)

func TestNewToolPermissionContext(t *testing.T) {
	ctx := NewToolPermissionContext()

	if ctx.Mode != PermissionModeDefault {
		t.Errorf("Expected mode 'default', got %s", ctx.Mode)
	}

	if ctx.AdditionalWorkingDirectories == nil {
		t.Error("Expected additional working directories to be initialized")
	}

	if ctx.AlwaysAllowRules == nil {
		t.Error("Expected always allow rules to be initialized")
	}

	if ctx.AlwaysDenyRules == nil {
		t.Error("Expected always deny rules to be initialized")
	}

	if ctx.AlwaysAskRules == nil {
		t.Error("Expected always ask rules to be initialized")
	}

	if ctx.IsBypassPermissionsModeAvailable {
		t.Error("Expected bypass mode to be unavailable by default")
	}
}

func TestGetEmptyToolPermissionContext(t *testing.T) {
	ctx := GetEmptyToolPermissionContext()

	if ctx == nil {
		t.Fatal("Expected non-nil context")
	}

	if ctx.Mode != PermissionModeDefault {
		t.Errorf("Expected mode 'default', got %s", ctx.Mode)
	}
}

func TestToolPermissionContext_Mode(t *testing.T) {
	ctx := NewToolPermissionContext()

	if ctx.GetMode() != PermissionModeDefault {
		t.Errorf("Expected mode 'default', got %s", ctx.GetMode())
	}

	ctx.SetMode(PermissionModeAuto)
	if ctx.GetMode() != PermissionModeAuto {
		t.Errorf("Expected mode 'auto', got %s", ctx.GetMode())
	}

	ctx.SetMode(PermissionModeBypass)
	if ctx.GetMode() != PermissionModeBypass {
		t.Errorf("Expected mode 'bypass', got %s", ctx.GetMode())
	}

	ctx.SetMode(PermissionModePlan)
	if ctx.GetMode() != PermissionModePlan {
		t.Errorf("Expected mode 'plan', got %s", ctx.GetMode())
	}
}

func TestToolPermissionContext_AdditionalWorkingDirectories(t *testing.T) {
	ctx := NewToolPermissionContext()

	dir1 := AdditionalWorkingDirectory{
		Path:        "/test/dir1",
		Description: "Test directory 1",
	}

	dir2 := AdditionalWorkingDirectory{
		Path:        "/test/dir2",
		Description: "Test directory 2",
	}

	// Test adding directories
	ctx.AddAdditionalWorkingDirectory("/test/dir1", dir1)
	ctx.AddAdditionalWorkingDirectory("/test/dir2", dir2)

	// Test getting directory
	retrieved, ok := ctx.GetAdditionalWorkingDirectory("/test/dir1")
	if !ok {
		t.Error("Expected to find directory")
	}
	if retrieved.Path != "/test/dir1" {
		t.Errorf("Expected path '/test/dir1', got %s", retrieved.Path)
	}

	// Test getting all directories
	all := ctx.GetAllAdditionalWorkingDirectories()
	if len(all) != 2 {
		t.Errorf("Expected 2 directories, got %d", len(all))
	}

	// Test removing directory
	ctx.RemoveAdditionalWorkingDirectory("/test/dir1")
	_, ok = ctx.GetAdditionalWorkingDirectory("/test/dir1")
	if ok {
		t.Error("Expected directory to be removed")
	}

	all = ctx.GetAllAdditionalWorkingDirectories()
	if len(all) != 1 {
		t.Errorf("Expected 1 directory, got %d", len(all))
	}
}

func TestToolPermissionContext_Rules(t *testing.T) {
	ctx := NewToolPermissionContext()

	rule1 := PermissionRule{
		Pattern:     "*.ts",
		Description: "TypeScript files",
		Enabled:     true,
	}

	rule2 := PermissionRule{
		Pattern:     "*.go",
		Description: "Go files",
		Enabled:     true,
	}

	// Test adding allow rules
	ctx.AddAlwaysAllowRule("source1", rule1)
	ctx.AddAlwaysAllowRule("source1", rule2)

	allowRules := ctx.GetAlwaysAllowRules()
	if len(allowRules["source1"]) != 2 {
		t.Errorf("Expected 2 allow rules, got %d", len(allowRules["source1"]))
	}

	// Test adding deny rules
	ctx.AddAlwaysDenyRule("source2", rule1)

	denyRules := ctx.GetAlwaysDenyRules()
	if len(denyRules["source2"]) != 1 {
		t.Errorf("Expected 1 deny rule, got %d", len(denyRules["source2"]))
	}

	// Test adding ask rules
	ctx.AddAlwaysAskRule("source3", rule1)

	askRules := ctx.GetAlwaysAskRules()
	if len(askRules["source3"]) != 1 {
		t.Errorf("Expected 1 ask rule, got %d", len(askRules["source3"]))
	}
}

func TestToolPermissionContext_Flags(t *testing.T) {
	ctx := NewToolPermissionContext()

	// Test bypass available
	if ctx.IsBypassAvailable() {
		t.Error("Expected bypass to be unavailable initially")
	}
	ctx.SetBypassAvailable(true)
	if !ctx.IsBypassAvailable() {
		t.Error("Expected bypass to be available")
	}

	// Test auto available
	if ctx.IsAutoAvailable() {
		t.Error("Expected auto to be unavailable initially")
	}
	ctx.SetAutoAvailable(true)
	if !ctx.IsAutoAvailable() {
		t.Error("Expected auto to be available")
	}

	// Test should avoid prompts
	if ctx.ShouldAvoidPrompts() {
		t.Error("Expected to not avoid prompts initially")
	}
	ctx.SetShouldAvoidPrompts(true)
	if !ctx.ShouldAvoidPrompts() {
		t.Error("Expected to avoid prompts")
	}

	// Test await automated checks
	if ctx.ShouldAwaitAutomatedChecks() {
		t.Error("Expected to not await automated checks initially")
	}
	ctx.SetAwaitAutomatedChecks(true)
	if !ctx.ShouldAwaitAutomatedChecks() {
		t.Error("Expected to await automated checks")
	}
}

func TestToolPermissionContext_PrePlanMode(t *testing.T) {
	ctx := NewToolPermissionContext()

	if ctx.GetPrePlanMode() != nil {
		t.Error("Expected pre-plan mode to be nil initially")
	}

	mode := PermissionModeAuto
	ctx.SetPrePlanMode(&mode)

	retrieved := ctx.GetPrePlanMode()
	if retrieved == nil {
		t.Fatal("Expected pre-plan mode to be set")
	}

	if *retrieved != PermissionModeAuto {
		t.Errorf("Expected pre-plan mode 'auto', got %s", *retrieved)
	}

	ctx.SetPrePlanMode(nil)
	if ctx.GetPrePlanMode() != nil {
		t.Error("Expected pre-plan mode to be nil after clearing")
	}
}

func TestToolPermissionContext_Clone(t *testing.T) {
	ctx := NewToolPermissionContext()
	ctx.SetMode(PermissionModeAuto)
	ctx.SetBypassAvailable(true)
	ctx.SetAutoAvailable(true)

	dir := AdditionalWorkingDirectory{
		Path:        "/test/dir",
		Description: "Test directory",
	}
	ctx.AddAdditionalWorkingDirectory("/test/dir", dir)

	rule := PermissionRule{
		Pattern:     "*.ts",
		Description: "TypeScript files",
		Enabled:     true,
	}
	ctx.AddAlwaysAllowRule("source1", rule)
	ctx.AddAlwaysDenyRule("source2", rule)
	ctx.AddAlwaysAskRule("source3", rule)

	mode := PermissionModeBypass
	ctx.SetPrePlanMode(&mode)

	// Clone the context
	cloned := ctx.Clone()

	// Verify all fields are copied
	if cloned.GetMode() != PermissionModeAuto {
		t.Errorf("Expected mode 'auto', got %s", cloned.GetMode())
	}

	if !cloned.IsBypassAvailable() {
		t.Error("Expected bypass to be available")
	}

	if !cloned.IsAutoAvailable() {
		t.Error("Expected auto to be available")
	}

	retrievedDir, ok := cloned.GetAdditionalWorkingDirectory("/test/dir")
	if !ok {
		t.Error("Expected to find directory in clone")
	}
	if retrievedDir.Path != "/test/dir" {
		t.Errorf("Expected path '/test/dir', got %s", retrievedDir.Path)
	}

	allowRules := cloned.GetAlwaysAllowRules()
	if len(allowRules["source1"]) != 1 {
		t.Errorf("Expected 1 allow rule in clone, got %d", len(allowRules["source1"]))
	}

	denyRules := cloned.GetAlwaysDenyRules()
	if len(denyRules["source2"]) != 1 {
		t.Errorf("Expected 1 deny rule in clone, got %d", len(denyRules["source2"]))
	}

	askRules := cloned.GetAlwaysAskRules()
	if len(askRules["source3"]) != 1 {
		t.Errorf("Expected 1 ask rule in clone, got %d", len(askRules["source3"]))
	}

	prePlanMode := cloned.GetPrePlanMode()
	if prePlanMode == nil {
		t.Fatal("Expected pre-plan mode to be set in clone")
	}
	if *prePlanMode != PermissionModeBypass {
		t.Errorf("Expected pre-plan mode 'bypass', got %s", *prePlanMode)
	}

	// Modify clone and verify original is unchanged
	cloned.SetMode(PermissionModeDefault)
	if ctx.GetMode() != PermissionModeAuto {
		t.Error("Expected original mode to be unchanged")
	}

	cloned.AddAlwaysAllowRule("source1", rule)
	originalAllowRules := ctx.GetAlwaysAllowRules()
	if len(originalAllowRules["source1"]) != 1 {
		t.Error("Expected original allow rules to be unchanged")
	}
}

func TestToolPermissionContext_Concurrency(t *testing.T) {
	ctx := NewToolPermissionContext()
	var wg sync.WaitGroup

	// Test concurrent mode changes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			modes := []PermissionMode{
				PermissionModeDefault,
				PermissionModeAuto,
				PermissionModeBypass,
				PermissionModePlan,
			}
			ctx.SetMode(modes[idx%4])
		}(i)
	}

	wg.Wait()

	// Verify mode is one of the valid values
	mode := ctx.GetMode()
	validModes := map[PermissionMode]bool{
		PermissionModeDefault: true,
		PermissionModeAuto:    true,
		PermissionModeBypass:  true,
		PermissionModePlan:    true,
	}
	if !validModes[mode] {
		t.Errorf("Expected valid mode, got %s", mode)
	}

	// Test concurrent directory additions
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			dir := AdditionalWorkingDirectory{
				Path:        string(rune('a' + idx)),
				Description: "Test",
			}
			ctx.AddAdditionalWorkingDirectory(string(rune('a'+idx)), dir)
		}(i)
	}

	wg.Wait()

	all := ctx.GetAllAdditionalWorkingDirectories()
	if len(all) != 100 {
		t.Errorf("Expected 100 directories, got %d", len(all))
	}
}

func TestPermissionRule(t *testing.T) {
	rule := PermissionRule{
		Pattern:     "*.ts",
		Description: "TypeScript files",
		Enabled:     true,
	}

	if rule.Pattern != "*.ts" {
		t.Errorf("Expected pattern '*.ts', got %s", rule.Pattern)
	}

	if rule.Description != "TypeScript files" {
		t.Errorf("Expected description 'TypeScript files', got %s", rule.Description)
	}

	if !rule.Enabled {
		t.Error("Expected rule to be enabled")
	}
}

func TestAdditionalWorkingDirectory(t *testing.T) {
	dir := AdditionalWorkingDirectory{
		Path:        "/test/dir",
		Description: "Test directory",
	}

	if dir.Path != "/test/dir" {
		t.Errorf("Expected path '/test/dir', got %s", dir.Path)
	}

	if dir.Description != "Test directory" {
		t.Errorf("Expected description 'Test directory', got %s", dir.Description)
	}
}
