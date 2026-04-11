package teleport

type EnvironmentKind string
type EnvironmentState string

const (
	EnvironmentKindAnthropicCloud EnvironmentKind = "anthropic_cloud"
	EnvironmentKindBYOC           EnvironmentKind = "byoc"
	EnvironmentKindBridge         EnvironmentKind = "bridge"

	EnvironmentStateActive EnvironmentState = "active"
)

type EnvironmentResource struct {
	Kind          EnvironmentKind  `json:"kind"`
	EnvironmentID string           `json:"environment_id"`
	Name          string           `json:"name"`
	CreatedAt     string           `json:"created_at"`
	State         EnvironmentState `json:"state"`
}

type EnvironmentSelectionInfo struct {
	AvailableEnvironments []EnvironmentResource
	SelectedEnvironment   *EnvironmentResource
	SelectedSource        string
}

func SelectEnvironment(environments []EnvironmentResource, defaultEnvironmentID string, configuredSource string) EnvironmentSelectionInfo {
	info := EnvironmentSelectionInfo{
		AvailableEnvironments: environments,
	}
	if len(environments) == 0 {
		return info
	}
	selected := &environments[0]
	for i := range environments {
		if environments[i].Kind != EnvironmentKindBridge {
			selected = &environments[i]
			break
		}
	}
	if defaultEnvironmentID != "" {
		for i := range environments {
			if environments[i].EnvironmentID == defaultEnvironmentID {
				selected = &environments[i]
				info.SelectedSource = configuredSource
				break
			}
		}
	}
	info.SelectedEnvironment = selected
	return info
}
