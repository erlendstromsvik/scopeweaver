package server

import (
	"io"
	"net/http"

	"github.com/Autumn-27/artex/locale"
)

const updateLocalOnlyMessage = "Update checks and downloads are unavailable while local_llm_only is enabled"

// Guard all network-producing update operations, including HTTP redirects and
// downloads that started before the policy changed. Existing staging cleanup
// consumes read errors and the update hub publishes the resulting failure.
func (s *Server) updatePolicyError() error {
	if strict, _ := s.effectiveLLMPolicy(); strict {
		return locale.NewError(updateLocalOnlyMessage)
	}
	return nil
}
func (s *Server) guardedUpdateClient(client *http.Client) *http.Client {
	copyClient := *client
	transport := copyClient.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	copyClient.Transport = &updatePolicyTransport{s: s, inner: transport}
	redirects := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := s.updatePolicyError(); err != nil {
			return err
		}
		if redirects != nil {
			return redirects(req, via)
		}
		if len(via) >= 10 {
			return locale.NewError("Too many redirects")
		}
		return nil
	}
	return &copyClient
}

type updatePolicyTransport struct {
	s     *Server
	inner http.RoundTripper
}

func (t *updatePolicyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.s.updatePolicyError(); err != nil {
		return nil, err
	}
	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if err := t.s.updatePolicyError(); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body = &updatePolicyBody{ReadCloser: resp.Body, s: t.s}
	return resp, nil
}

type updatePolicyBody struct {
	io.ReadCloser
	s *Server
}

func (b *updatePolicyBody) Read(p []byte) (int, error) {
	if err := b.s.updatePolicyError(); err != nil {
		return 0, err
	}
	n, err := b.ReadCloser.Read(p)
	// If policy changed while blocked in a read, discard those bytes before the
	// downloader can write them or interpret EOF as successful completion.
	if policyErr := b.s.updatePolicyError(); policyErr != nil {
		return 0, policyErr
	}
	return n, err
}
