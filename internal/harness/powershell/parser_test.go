package powershell

import "testing"

func TestParseCommandExtractsStatementsVariablesAndStopParsing(t *testing.T) {
	parsed := ParseCommand(`Get-ChildItem $HOME | Select-Object Name; Write-Host --% raw text`)
	if !parsed.Valid {
		t.Fatalf("expected parse success, got %#v", parsed.Errors)
	}
	if !parsed.HasStopParsing {
		t.Fatal("expected stop-parsing marker to be detected")
	}
	if len(parsed.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %#v", parsed.Statements)
	}
	if len(parsed.Variables) == 0 || parsed.Variables[0].Path != "HOME" {
		t.Fatalf("expected HOME variable, got %#v", parsed.Variables)
	}
	if parsed.Statements[0].Commands[0].Name != "Get-ChildItem" {
		t.Fatalf("unexpected first command %#v", parsed.Statements[0].Commands[0])
	}
}

func TestGetAllCommandsIncludesNestedCommands(t *testing.T) {
	parsed := ParseCommand(`if ($true) { git status; npm test }`)
	commands := GetAllCommands(parsed)
	if len(commands) < 2 {
		t.Fatalf("expected nested commands, got %#v", commands)
	}
	if commands[0].Name != "git" || commands[1].Name != "npm" {
		t.Fatalf("unexpected commands %#v", commands)
	}
}

func TestGetCommandPrefixStatic(t *testing.T) {
	result := GetCommandPrefixStatic(`git -C repo status`)
	if result == nil || result.CommandPrefix != "git status" {
		t.Fatalf("expected git status prefix, got %#v", result)
	}

	cmdlet := GetCommandPrefixStatic(`Get-Process -Name pwsh`)
	if cmdlet == nil || cmdlet.CommandPrefix != "Get-Process" {
		t.Fatalf("expected Get-Process prefix, got %#v", cmdlet)
	}
}

func TestGetCompoundCommandPrefixesStaticCollapsesSharedRoots(t *testing.T) {
	prefixes := GetCompoundCommandPrefixesStatic(`npm run test; npm run lint`, nil)
	if len(prefixes) != 1 || prefixes[0] != "npm run" {
		t.Fatalf("expected collapsed npm run prefix, got %#v", prefixes)
	}
}
