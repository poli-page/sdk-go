package polipage_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/poli-page/sdk-go/option"
)

// phaseTransport makes a test's timer or cancel land in the phase the test is named after,
// however slowly the runner schedules the test server. Without it, a short SDK timeout or a
// cancel on a timer can end the call before the server even answers, and a test named
// "mid-body" then passes, or fails, on a phase it never reached.
//
// It sends each request to base on a context the SDK cannot end, so the server's answer
// always arrives, then:
//   - an API request (/v1/...) other than streamPath is read in full before RoundTrip
//     returns: a short SDK timeout can never end the render call that sets a download up;
//   - the streamPath request returns once its headers and its first prefetch body bytes are
//     in. The rest of its body is read under the request's own context, as net/http does: the
//     SDK's timer or the caller's cancel ends that read. So they can only land mid-body, after
//     the SDK has read the prefetched bytes. onRest, when set, runs once, as the SDK first
//     reads past them: a test's cancel placed there lands mid-body by construction;
//   - any other request (a storage download that never answers) goes to base unchanged.
//
// read counts the body bytes the SDK got from streamPath, so a test can check it got there.
type phaseTransport struct {
	base       http.RoundTripper
	streamPath string
	prefetch   int
	onRest     func()
	read       atomic.Int64
}

// phaseClient sends the client's requests to server through p.
func phaseClient(server *httptest.Server, p *phaseTransport) option.RequestOption {
	p.base = server.Client().Transport
	return option.WithHTTPClient(&http.Client{Transport: p})
}

func (p *phaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	stream := p.streamPath != "" && req.URL.Path == p.streamPath
	if !stream && !strings.HasPrefix(req.URL.Path, "/v1/") {
		return p.base.RoundTrip(req)
	}
	ctx := req.Context()
	resp, err := p.base.RoundTrip(req.WithContext(context.WithoutCancel(ctx)))
	if err != nil {
		return nil, err
	}
	if !stream {
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(data))
		return resp, nil
	}
	head := make([]byte, p.prefetch)
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	rest := resp.Body
	body := &midBody{head: head, rest: rest, ctx: ctx, transport: p}
	// The request's context ending closes the connection, which ends a read in progress.
	body.stop = context.AfterFunc(ctx, func() { _ = rest.Close() })
	resp.Body = body
	return resp, nil
}

// midBody serves the prefetched bytes, then the rest of the body under ctx.
type midBody struct {
	head      []byte
	rest      io.ReadCloser
	ctx       context.Context
	transport *phaseTransport
	once      sync.Once
	stop      func() bool
}

func (b *midBody) Read(p []byte) (int, error) {
	if len(b.head) > 0 {
		n := copy(p, b.head)
		b.head = b.head[n:]
		b.transport.read.Add(int64(n))
		return n, nil
	}
	b.once.Do(func() {
		if b.transport.onRest != nil {
			b.transport.onRest()
		}
	})
	if b.ctx.Err() != nil {
		return 0, context.Cause(b.ctx)
	}
	n, err := b.rest.Read(p)
	b.transport.read.Add(int64(n))
	if err != nil && b.ctx.Err() != nil {
		return n, context.Cause(b.ctx)
	}
	return n, err
}

func (b *midBody) Close() error {
	b.stop()
	return b.rest.Close()
}
