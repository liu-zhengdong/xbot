package gateway

import _ "embed"

// faviconSVG is the XBot mark served at /favicon.svg and /favicon.ico.
//
//go:embed favicon.svg
var faviconSVG []byte
