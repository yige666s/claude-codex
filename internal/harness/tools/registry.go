package tools

import (
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
)

type Registry struct {
	byName   map[string]Tool
	order    []Tool
	disabled map[string]bool
}

func NewRegistry(toolList ...Tool) *Registry {
	r := &Registry{
		byName:   make(map[string]Tool, len(toolList)),
		order:    make([]Tool, 0, len(toolList)),
		disabled: make(map[string]bool),
	}
	for _, tool := range toolList {
		r.byName[tool.Name()] = tool
		r.order = append(r.order, tool)
	}
	return r
}

func (r *Registry) Register(tool Tool) {
	if _, exists := r.byName[tool.Name()]; !exists {
		r.order = append(r.order, tool)
	}
	r.byName[tool.Name()] = tool
}

func (r *Registry) Unregister(name string) {
	delete(r.byName, name)
	for i, t := range r.order {
		if t.Name() == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

func (r *Registry) Enable(name string)  { delete(r.disabled, name) }
func (r *Registry) Disable(name string) { r.disabled[name] = true }

func (r *Registry) Get(name string) (Tool, error) {
	tool, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", name)
	}
	if r.disabled[name] {
		return nil, fmt.Errorf("tool %q is disabled", name)
	}
	return tool, nil
}

// Descriptors returns descriptors for all enabled tools.
func (r *Registry) Descriptors() []Descriptor {
	out := make([]Descriptor, 0, len(r.order))
	for _, tool := range r.order {
		if !r.disabled[tool.Name()] {
			out = append(out, Describe(tool))
		}
	}
	return out
}

// DescriptorsForLevel returns descriptors for tools at or below the given permission level.
func (r *Registry) DescriptorsForLevel(level permissions.Level) []Descriptor {
	out := make([]Descriptor, 0, len(r.order))
	for _, tool := range r.order {
		if r.disabled[tool.Name()] {
			continue
		}
		if tool.Permission() <= level {
			out = append(out, Describe(tool))
		}
	}
	return out
}
