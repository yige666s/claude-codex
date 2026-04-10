package securestorage

// Data is the JSON payload persisted by secure storage backends.
//
// It intentionally stays schema-light for now so different callers can persist
// OAuth, plugin, or MCP secret material without forcing a shared migration.
type Data map[string]any

// Store is the common contract shared by secure storage backends.
type Store interface {
	Name() string
	Read() (Data, error)
	Write(Data) (WriteResult, error)
	Delete() error
}

// WriteResult describes a successful write outcome.
type WriteResult struct {
	Warning string
}
