// Package knowledge embeds the builtin compatibility rule files.
// Evaluation lives in internal/knowledge so this package stays data-only.
package knowledge

import "embed"

// Rules is the builtin knowledge base shipped with the binary.
//
//go:embed rules/*.yaml
var Rules embed.FS

// PlutoVersions is the pinned Fairwinds Pluto versions.yaml.
//
//go:embed pluto/versions.yaml
var PlutoVersions []byte

// PlutoSource names the Pluto release that PlutoVersions was copied from.
//
//go:embed pluto/source.yaml
var PlutoSource []byte
