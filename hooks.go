package polipage

import "github.com/poli-page/sdk-go/internal/clientconfig"

// RequestEvent is delivered to the OnRequest hook before each HTTP
// attempt. Aliased to [clientconfig.RequestEvent] so option.WithOnRequest
// and the re-export here both spell the type identically.
type RequestEvent = clientconfig.RequestEvent

// ResponseEvent is delivered to the OnResponse hook after each
// successful 2xx response. Aliased to [clientconfig.ResponseEvent].
type ResponseEvent = clientconfig.ResponseEvent
