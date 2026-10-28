package agentruntime

import (
	"context"
	"errors"
	"path"
	"strings"
)

// remoteSkillArtifactWriter is scoped to the prepared task context and to one
// final output filename. It never reads a same-named API-host workspace file.
type remoteSkillArtifactWriter struct {
	base      ArtifactWriter
	collector RemoteSkillCollector
	request   RemoteSkillRequest
}

func (w remoteSkillArtifactWriter) Write(ctx context.Context, filename, contentType string, data []byte) (*Artifact, error) {
	return w.base.Write(ctx, filename, contentType, data)
}
func (w remoteSkillArtifactWriter) ReadArtifactFile(ctx context.Context, requested string) ([]byte, error) {
	requested = strings.TrimSpace(strings.ReplaceAll(requested, "\\", "/"))
	if requested == "" || strings.HasPrefix(requested, "/") || path.Clean(requested) != requested {
		return nil, errors.New("remote artifact path rejected")
	}
	parts := strings.Split(requested, "/")
	if len(parts) != 2 || (parts[0] != "outputs" && parts[0] != "generated-artifacts") || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return nil, errors.New("remote artifact path not a final output")
	}
	files, err := w.collector.CollectSkillArtifacts(ctx, w.request)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.Filename == parts[1] {
			return append([]byte(nil), file.Content...), nil
		}
	}
	return nil, errors.New("remote artifact not found in verified outputs")
}
