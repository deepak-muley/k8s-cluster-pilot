// Package knowledge embeds the builtin compatibility rule files.
// Evaluation lives in internal/knowledge so this package stays data-only.
package knowledge

import "embed"

// Rules is the builtin knowledge base shipped with the binary.
//
//go:embed rules/*.yaml
var Rules embed.FS
