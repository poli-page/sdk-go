package polipage

import "github.com/poli-page/sdk-go/internal/clientconfig"

// RequestEvent is delivered to the OnRequest hook before each HTTP
// attempt. Aliased to [clientconfig.RequestEvent] so option.WithOnRequest
// and the re-export here both spell the type identically.
type RequestEvent = clientconfig.RequestEvent

// ResponseEvent is delivered to the OnResponse hook once per 2xx attempt,
// after its body has been read in full; never for a 2xx whose body read
// fails. DurationMs runs from just before the request is sent until the
// body is read. Aliased to [clientconfig.ResponseEvent].
type ResponseEvent = clientconfig.ResponseEvent
