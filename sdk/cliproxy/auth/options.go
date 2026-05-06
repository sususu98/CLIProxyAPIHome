package auth

import (
	"net/http"
	"net/url"
)

// RequestedModelMetadataKey stores the client-requested model name in Options.Metadata.
// It is used to preserve the original model string across prefix rewriting / alias resolution.
const RequestedModelMetadataKey = "requested_model"

// Options carries optional request hints used during dispatch selection.
//
// This is a deliberately small subset of CPA's execution options: CLIProxyAPIHome only needs
// headers / query / raw request bytes for selector decisions, and a generic metadata bag.
type Options struct {
	Headers         http.Header
	Query           url.Values
	OriginalRequest []byte
	Metadata        map[string]any
}
