package core

import _ "embed"

// Rules is the shipped catalog: the only list of what can be cleaned.
//
//go:embed rules.json
var Rules []byte
