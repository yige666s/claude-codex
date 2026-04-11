package settings

type SettingSource string

const (
	SourceUser    SettingSource = "userSettings"
	SourceProject SettingSource = "projectSettings"
	SourceLocal   SettingSource = "localSettings"
	SourceFlag    SettingSource = "flagSettings"
	SourcePolicy  SettingSource = "policySettings"
)

var SettingSources = []SettingSource{
	SourceUser,
	SourceProject,
	SourceLocal,
	SourceFlag,
	SourcePolicy,
}

type EditableSettingSource string

const (
	EditableUser    EditableSettingSource = EditableSettingSource(SourceUser)
	EditableProject EditableSettingSource = EditableSettingSource(SourceProject)
	EditableLocal   EditableSettingSource = EditableSettingSource(SourceLocal)
)

type Document map[string]any

type ValidationError struct {
	File         string
	Path         string
	Message      string
	Expected     string
	InvalidValue any
	Suggestion   string
	DocLink      string
}

type SettingsWithErrors struct {
	Settings Document
	Errors   []ValidationError
}

type SourceSnapshot struct {
	Source  SettingSource
	Path    string
	Exists  bool
	ModTime int64
	Size    int64
}

type ChangeEvent struct {
	Source SettingSource
	Path   string
	Type   string
}

var CustomizationSurfaces = []string{"skills", "agents", "hooks", "mcp"}
