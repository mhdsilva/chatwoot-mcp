package chatwoot

import "chatwoot-mcp/internal/core"

// Compile-time proof that the concrete Application API client implements the
// full core.API surface. A missing override fails this build.
var _ core.API = (*Client)(nil)
