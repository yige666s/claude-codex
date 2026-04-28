package swarm

import "testing"

func TestBackendRegistrySelectsExplicitAndAutoBackends(t *testing.T) {
	registry := NewBackendRegistry(BackendEnvironment{
		InsideTmux:     true,
		InITerm2:       true,
		TmuxAvailable:  true,
		ITermAvailable: true,
	})

	if got, err := registry.Select(BackendSelection{Mode: TeammateModeInProcess}); err != nil || got.Executor.Type() != BackendTypeInProcess {
		t.Fatalf("explicit in-process Select() = (%v, %v), want in-process", got.Executor.Type(), err)
	}
	if got, err := registry.Select(BackendSelection{Mode: TeammateModeTmux}); err != nil || got.Executor.Type() != BackendTypeTmux {
		t.Fatalf("explicit tmux Select() = (%v, %v), want tmux", got.Executor.Type(), err)
	}
	if got, err := registry.Select(BackendSelection{Mode: TeammateModeITerm2}); err != nil || got.Executor.Type() != BackendTypeITerm2 {
		t.Fatalf("explicit iterm2 Select() = (%v, %v), want iterm2", got.Executor.Type(), err)
	}

	got, err := registry.Select(BackendSelection{Mode: TeammateModeAuto})
	if err != nil {
		t.Fatalf("auto Select() error = %v", err)
	}
	if got.Executor.Type() != BackendTypeTmux || !got.IsNative {
		t.Fatalf("auto inside tmux = %+v, want native tmux", got)
	}
}

func TestBackendRegistryAutoFallbacks(t *testing.T) {
	t.Run("non-interactive always in-process", func(t *testing.T) {
		registry := NewBackendRegistry(BackendEnvironment{
			TmuxAvailable:  true,
			NonInteractive: true,
		})
		got, err := registry.Select(BackendSelection{Mode: TeammateModeAuto})
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		if got.Executor.Type() != BackendTypeInProcess || got.FallbackReason == "" {
			t.Fatalf("Select() = %+v, want in-process fallback reason", got)
		}
	})

	t.Run("iterm without it2 falls back to tmux and requests setup", func(t *testing.T) {
		registry := NewBackendRegistry(BackendEnvironment{
			InITerm2:       true,
			TmuxAvailable:  true,
			ITermAvailable: false,
		})
		got, err := registry.Select(BackendSelection{Mode: TeammateModeAuto})
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		if got.Executor.Type() != BackendTypeTmux || !got.NeedsITermSetup || got.IsNative {
			t.Fatalf("Select() = %+v, want tmux fallback with setup hint", got)
		}
	})

	t.Run("no pane backend falls back to in-process in auto mode", func(t *testing.T) {
		registry := NewBackendRegistry(BackendEnvironment{
			InITerm2:       true,
			TmuxAvailable:  false,
			ITermAvailable: false,
		})
		got, err := registry.Select(BackendSelection{Mode: TeammateModeAuto})
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		if got.Executor.Type() != BackendTypeInProcess || got.FallbackReason == "" {
			t.Fatalf("Select() = %+v, want in-process fallback", got)
		}
	})
}

func TestBackendRegistryExplicitUnavailableReturnsError(t *testing.T) {
	registry := NewBackendRegistry(BackendEnvironment{})
	if _, err := registry.Select(BackendSelection{Mode: TeammateModeTmux}); err == nil {
		t.Fatal("expected explicit tmux selection to fail when unavailable")
	}
	if _, err := registry.Select(BackendSelection{Mode: TeammateModeITerm2}); err == nil {
		t.Fatal("expected explicit iterm2 selection to fail when unavailable")
	}
}

func TestBuildPaneWorkerCommandUsesTemplate(t *testing.T) {
	t.Setenv("CLAUDE_CODE_TEAM_WORKER_CMD", "worker --id {agent_id} --name {name} --prompt {prompt}")
	cmd, err := buildPaneWorkerCommand(TeammateSpawnConfig{
		TeammateIdentity: TeammateIdentity{Name: "Builder", TeamName: "core"},
		Prompt:           "hello world",
	}, FormatAgentID("Builder", "core"))
	if err != nil {
		t.Fatalf("buildPaneWorkerCommand() error = %v", err)
	}
	want := "worker --id 'Builder@core' --name 'Builder' --prompt 'hello world'"
	if cmd != want {
		t.Fatalf("command = %q, want %q", cmd, want)
	}
}
