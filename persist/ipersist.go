package persist

import "context"

// IPersist is the base persistence interface. All storage backends implement it.
// Format-specific stores (MarkdownPersist, JSONLPersist, VectorPersist) embed an
// IPersist backend and add format-aware methods on top.
type IPersist interface {
	Store(ctx context.Context, key string, data []byte) error
	Load(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Exists(ctx context.Context, key string) bool
}
