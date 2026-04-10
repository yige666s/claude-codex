package powershell

import "strings"

var (
	FilePathExecutionCmdlets = map[string]struct{}{
		"invoke-command":        {},
		"start-job":             {},
		"start-threadjob":       {},
		"register-scheduledjob": {},
	}
	DangerousScriptBlockCmdlets = map[string]struct{}{
		"invoke-command":        {},
		"invoke-expression":     {},
		"start-job":             {},
		"start-threadjob":       {},
		"register-scheduledjob": {},
		"register-engineevent":  {},
		"register-objectevent":  {},
		"register-wmievent":     {},
		"new-pssession":         {},
		"enter-pssession":       {},
	}
	ModuleLoadingCmdlets = map[string]struct{}{
		"import-module":  {},
		"ipmo":           {},
		"install-module": {},
		"save-module":    {},
		"update-module":  {},
		"install-script": {},
		"save-script":    {},
	}
	NetworkCmdlets = map[string]struct{}{
		"invoke-webrequest": {},
		"invoke-restmethod": {},
	}
	AliasHijackCmdlets = map[string]struct{}{
		"set-alias":    {},
		"sal":          {},
		"new-alias":    {},
		"nal":          {},
		"set-variable": {},
		"sv":           {},
		"new-variable": {},
		"nv":           {},
	}
	WMICIMCmdlets = map[string]struct{}{
		"invoke-wmimethod": {},
		"iwmi":             {},
		"invoke-cimmethod": {},
	}
	ArgGatedCmdlets = map[string]struct{}{
		"select-object":  {},
		"sort-object":    {},
		"group-object":   {},
		"where-object":   {},
		"measure-object": {},
		"write-output":   {},
		"write-host":     {},
		"start-sleep":    {},
		"format-table":   {},
		"format-list":    {},
		"format-wide":    {},
		"format-custom":  {},
		"out-string":     {},
		"out-host":       {},
		"ipconfig":       {},
		"hostname":       {},
		"route":          {},
	}
	NeverSuggest = map[string]struct{}{
		"pwsh":           {},
		"powershell":     {},
		"cmd":            {},
		"bash":           {},
		"wsl":            {},
		"sh":             {},
		"start-process":  {},
		"start":          {},
		"add-type":       {},
		"new-object":     {},
		"foreach-object": {},
		"node":           {},
		"python":         {},
		"python3":        {},
		"ruby":           {},
		"perl":           {},
		"deno":           {},
		"bun":            {},
		"go":             {},
	}
)

func init() {
	for _, set := range []map[string]struct{}{
		FilePathExecutionCmdlets,
		DangerousScriptBlockCmdlets,
		ModuleLoadingCmdlets,
		NetworkCmdlets,
		AliasHijackCmdlets,
		WMICIMCmdlets,
		ArgGatedCmdlets,
	} {
		for name := range set {
			NeverSuggest[name] = struct{}{}
		}
	}
}

func NeverSuggestCommand(name string) bool {
	_, ok := NeverSuggest[strings.ToLower(strings.TrimSpace(name))]
	return ok
}
