package schemas

import _ "embed"

// CallHECInput is the published call_hec input schema.
//
//go:embed call-hec.input.json
var CallHECInput []byte

// CallHECOutput is the stable call_hec result schema.
//
//go:embed call-hec.output.json
var CallHECOutput []byte
