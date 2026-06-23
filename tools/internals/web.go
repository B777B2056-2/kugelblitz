package internals

import "github.com/B777B2056-2/kugelblitz/tools"

// RegisterWebTools registers web_search and web_fetch into the global tool registry.
//
// Passing nil uses DuckDuckGo for web_search (free, no API key required).
// To use a custom backend, pass a *WebSearchConfig with your own SearchBackend.
//
// Usage:
//
//	internals.RegisterWebTools(nil) // default DuckDuckGo
func RegisterWebTools(cfg *WebSearchConfig) {
	tools.Register(&WebFetch{})
	tools.Register(newWebSearch(cfg))
}
