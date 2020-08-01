package mock

import (
	"github.com/thewug/reqtify"

	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
)

var ErrNoHandler error = errors.New("ReqtifierMock received a request it was not expecting")

type ReqtifyAnalyzer func(req *RequestMock) (*http.Response, error)

type ReqtifierMock struct {
	FakeReqtifier *reqtify.ReqtifierImpl

	analyzeFunc ReqtifyAnalyzer
}

func (this *ReqtifierMock) New(endpoint string) (reqtify.Request) {
	return &RequestMock{
		RequestImpl: reqtify.RequestImpl{
			URLPath: endpoint,
			Verb: reqtify.GET,
			QueryParams: url.Values{},
			FormParams: url.Values{},
			AutoParams: url.Values{},
			FormFiles: make(map[string][]reqtify.FormFile),
			Headers: make(map[string]string),
			ReqClient: this.FakeReqtifier,
		},
		Mock: this,
	}
}

func (this *ReqtifierMock) AnalyzeWith(f ReqtifyAnalyzer) {
	this.analyzeFunc = f
}

type ResponseAndError struct {
	Response *http.Response
	Error error
}

type MockReqtifyRequestExaminer struct {
	Requests <-chan *RequestMock
	Responses chan<- ResponseAndError
}

func (this *ReqtifierMock) Examine() MockReqtifyRequestExaminer {
	requestReader := make(chan *RequestMock)
	responseWriter := make(chan ResponseAndError)

	this.AnalyzeWith(func(req *RequestMock) (*http.Response, error) {
		requestReader <- req
		response := <- responseWriter
		return response.Response, response.Error
	})

	return MockReqtifyRequestExaminer{
		Requests: requestReader,
		Responses: responseWriter,
	}
}

type RequestMock struct {
	reqtify.RequestImpl

	Mock *ReqtifierMock
}

func (this *RequestMock) Do() (*http.Response, error) {
	if this.Mock.analyzeFunc != nil {
		resp, errrrrrrr := this.Mock.analyzeFunc(this)

		// Packing into response, if we have one
		if len(this.RequestImpl.Response)!= 0 {
			var body []byte
			var err error
			if resp != nil {
				body, err = ioutil.ReadAll(resp.Body)
				defer resp.Body.Close()
			}
			if err != nil {
				return nil, err
			}
			for _, response := range this.RequestImpl.Response {
				e := response.Unmarshal(body)
				if err != nil {
					err = e
				}
			}
		}

		return resp, errrrrrrr
	}

	return nil, ErrNoHandler
}

func (this *RequestMock) Method(v reqtify.HttpVerb) (reqtify.Request) {
	this.RequestImpl.Method(v)
	return this
}

func (this *RequestMock) Path(path string) (reqtify.Request) {
	this.RequestImpl.Path(path)
	return this
}

func (this *RequestMock) Header(key, value string) (reqtify.Request) {
	this.RequestImpl.Header(key, value)
	return this
}

func (this *RequestMock) Cookie(c *http.Cookie) (reqtify.Request) {
	this.RequestImpl.Cookie(c)
	return this
}

func (this *RequestMock) BasicAuthentication(user, password string) (reqtify.Request) {
	this.RequestImpl.BasicAuthentication(user, password)
	return this
}

func (this *RequestMock) Multipart() (reqtify.Request) {
	this.RequestImpl.Multipart()
	return this
}

func (this *RequestMock) Arg(key string, value interface{}) (reqtify.Request) {
	this.RequestImpl.Arg(key, value)
	return this
}

func (this *RequestMock) URLArg(key string, value interface{}) (reqtify.Request) {
	this.RequestImpl.URLArg(key, value)
	return this
}

func (this *RequestMock) FormArg(key string, value interface{}) (reqtify.Request) {
	this.RequestImpl.FormArg(key, value)
	return this
}

func (this *RequestMock) FileArg(key, filename string, data io.Reader) (reqtify.Request) {
	this.RequestImpl.FileArg(key, filename, data)
	return this
}

func (this *RequestMock) ArgDefault(key string, value, def interface{}) (reqtify.Request) {
	this.RequestImpl.ArgDefault(key, value, def)
	return this
}

func (this *RequestMock) URLArgDefault(key string, value, def interface{}) (reqtify.Request) {
	this.RequestImpl.URLArgDefault(key, value, def)
	return this
}

func (this *RequestMock) FormArgDefault(key string, value, def interface{}) (reqtify.Request) {
	this.RequestImpl.FormArgDefault(key, value, def)
	return this
}

func (this *RequestMock) Into(into reqtify.ResponseUnmarshaller) (reqtify.Request) {
	this.RequestImpl.Into(into)
	return this
}

func (this *RequestMock) JSONInto(into interface{}) (reqtify.Request) {
	this.RequestImpl.JSONInto(into)
	return this
}

func (this *RequestMock) XMLInto(into interface{}) (reqtify.Request) {
	this.RequestImpl.XMLInto(into)
	return this
}

func (this *RequestMock) DebugPrint() (reqtify.Request) {
	this.RequestImpl.DebugPrint()
	return this
}
