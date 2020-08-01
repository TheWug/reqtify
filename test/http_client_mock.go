package test

import (
	"net/http"
	"io"
	"strings"
	"errors"
	"net/url"
)

var ErrNoHandler error = errors.New("HttpClientMock received a request it was not expecting")

type HttpReqAnalyzer func(req *http.Request) (*http.Response, error)

type requestResponseNode struct {
	Request  *http.Request
	Response *http.Response
	Error    error

	next *requestResponseNode
}

type MockHttpClient struct {
	analyzeFunc HttpReqAnalyzer
}

func (this *MockHttpClient) AnalyzeWith(f HttpReqAnalyzer) {
	this.analyzeFunc = f
}

type ResponseAndError struct {
	Response *http.Response
	Error error
}

type MockHttpReqExaminer struct {
	Requests <-chan *http.Request
	Responses chan<- ResponseAndError
}

func (this *MockHttpClient) Examine() MockHttpReqExaminer {
	requestReader := make(chan *http.Request)
	responseWriter := make(chan ResponseAndError)

	this.AnalyzeWith(func(req *http.Request) (*http.Response, error) {
		requestReader <- req
		response := <- responseWriter
		return response.Response, response.Error
	})

	return MockHttpReqExaminer{
		Requests: requestReader,
		Responses: responseWriter,
	}
}

func (this *MockHttpClient) Do(req *http.Request) (*http.Response, error) {
	if this.analyzeFunc != nil {
		return this.analyzeFunc(req)
	}

	return nil, ErrNoHandler
}

/*
   The following functions are
   copied from net/http/client.go,
   some of them modified, in order
   to accurately simulate how a real
   http.Client behaves. 
   https://golang.org/src/net/http/client.go?s=1950:3998#L48
*/

func (c *MockHttpClient) Get(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c *MockHttpClient) Head(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c *MockHttpClient) Post(url, contentType string, body io.Reader) (resp *http.Response, err error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.Do(req)
}

func (c *MockHttpClient) PostForm(url string, data url.Values) (resp *http.Response, err error) {
	return c.Post(url, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
}

func (c *MockHttpClient) CloseIdleConnections() {
	return // a no-op since the mock has no actual connections
}
