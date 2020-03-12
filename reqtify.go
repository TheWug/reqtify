package reqtify

import (
	"time"
	"io"
	"net/http"
	"net/url"
	"io/ioutil"
	"encoding/json"
	"strings"
)

type HttpVerb string

const GET HttpVerb = "GET"
const POST HttpVerb = "POST"
const PUT HttpVerb = "PUT"
const PATCH HttpVerb = "PATCH"
const DELETE HttpVerb = "DELETE"
const HEAD HttpVerb = "HEAD"

type FormFile struct {
	Name string
	Data io.Reader
}

type ResponseError struct {
	StatusCode int
	StatusText string
}

func (r *ResponseError) Error() string {
	return r.StatusText
}	

type Reqtifier struct {
	Root         string
	RateLimiter *time.Ticker
	HttpClient  *http.Client
	LastChance   func(*Request) error
	AgentName    string
}

type Request struct {
	Path           string
	Verb           HttpVerb
	Response       interface{}
	QueryParams    url.Values
	FormParams     url.Values
	AutoParams     url.Values
	FormFiles      map[string][]FormFile
	Headers        map[string]string
	Cookies     []*http.Cookie
	ForceMultipart bool
	ReqClient     *Reqtifier
}

func New(root string, rl *time.Ticker, client *http.Client, lc func(*Request) (error), agent string) (Reqtifier) {
	r := Reqtifier{
		Root: root,
		RateLimiter: rl,
		HttpClient: client,
		LastChance: lc,
		AgentName: agent,
	}

	if r.HttpClient == nil {
		r.HttpClient = &http.Client{Transport: &http.Transport{} }
	}

	return r
}

func (this *Reqtifier) Do(req *Request) (*http.Response, error) {
	// wait for rate limiter to be ready
	if this.RateLimiter != nil { <- this.RateLimiter.C }

	// figure out request URL from query params and other stuff
	callURL := req.URL()

	// calculate request body
	var body io.Reader
	var bodytype string
	if req.Verb != GET {
		body, bodytype = req.GetBody()
	}

	r, err := http.NewRequest(string(req.Verb), callURL, body)
	if err != nil { return nil, err }

	// set headers
	for key, value := range req.Headers {
		r.Header.Add(key, value)
	}

	// override content-type header, if one was explicitly specified
	if bodytype != "" {
		r.Header.Add("Content-Type", bodytype)
	}

	// Add cookies
	for _, cookie := range req.Cookies {
		r.AddCookie(cookie)
	}

	resp, err := this.HttpClient.Do(r)
	if err != nil {
		return nil, err
	}

	// Packing into response, if we have one
	if req.Response != nil {
		body, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(body, req.Response)
		if err != nil {
			return resp, err
		}
	}

	// OK
	return resp, nil
}

func (this *Reqtifier) New(endpoint string) (*Request) {
	return &Request{
		Path: endpoint,
		Verb: GET,
		QueryParams: url.Values{},
		FormParams: url.Values{},
		AutoParams: url.Values{},
		FormFiles: make(map[string][]FormFile),
		Headers: make(map[string]string),
		ReqClient: this,
	}
}

func (this *Request) GetBody() (io.Reader, string) {
	if this.ForceMultipart || len(this.FormFiles) != 0 {
		var m multipartRequestBody
		for k, va := range this.FormParams {
			for _, v := range va {
				m.addParam(k, v)
			}
		}
		if this.Verb != GET {
			for k, va := range this.AutoParams {
				for _, v := range va {
					m.addParam(k, v)
				}
			}
		}
		for k, va := range this.FormFiles {
			for _, v := range va {
				m.addFileParam(k, v)
			}
		}
		m.close()
		return m.toReader(), m.contentType()
	} else {
		params := this.FormParams.Encode()
		if len(this.AutoParams) != 0 && this.Verb != GET {
			if len(params) != 0 {
				params += "&"
			}
			params += this.AutoParams.Encode()
		}
		return strings.NewReader(params), "application/x-www-form-urlencoded"
	}
}

func (this *Request) Method(v HttpVerb) (*Request) {
	this.Verb = v
	return this
}

func (this *Request) Into(into interface{}) (*Request) {
	this.Response = into
	return this
}

func (this *Request) Arg(key, value string) (*Request) {
	this.AutoParams.Add(key, value)
	return this
}

func (this *Request) URLArg(key, value string) (*Request) {
	this.QueryParams.Add(key, value)
	return this
}

func (this *Request) FormArg(key, value string) (*Request) {
	this.FormParams.Add(key, value)
	return this
}

func (this *Request) FileArg(key, filename string, data io.Reader) (*Request) {
	this.FormFiles[key] = append(this.FormFiles[key], FormFile{Name: filename, Data: data})
	return this
}

func (this *Request) ArgDefault(key, value, def string) (*Request) {
	if value != def {
		this.AutoParams.Add(key, value)
	}
	return this
}

func (this *Request) URLArgDefault(key, value, def string) (*Request) {
	if value != def {
		this.QueryParams.Add(key, value)
	}
	return this
}

func (this *Request) FormArgDefault(key, value, def string) (*Request) {
	if value != def {
		this.FormParams.Add(key, value)
	}
	return this
}

func (this *Request) Header(key, value string) (*Request) {
	this.Headers[strings.ToLower(key)] = value
	return this
}

func (this *Request) Cookie(c *http.Cookie) (*Request) {
	this.Cookies = append(this.Cookies, c)
	return this
}

func (this *Request) Multipart() (*Request) {
	this.ForceMultipart = true
	return this
}

func (this *Request) Target() (string) {
	return this.ReqClient.Root + this.Path
}

func (this *Request) URL() (string) {
	callURL := this.ReqClient.Root + this.Path
	params := this.QueryParams.Encode()
	if len(this.AutoParams) != 0 && this.Verb == GET {
		if len(params) != 0 {
			params += "&"
		}
		params += this.AutoParams.Encode()
	}

	if len(params) != 0 {
		callURL += "?" + params
	}

	return callURL
}

func (this *Request) Do() (*http.Response, error) {
	if len(this.ReqClient.AgentName) != 0 {
	        this.Header("User-Agent", this.ReqClient.AgentName)
	}

	if this.ReqClient.LastChance != nil {
		err := this.ReqClient.LastChance(this)
		if err != nil { return nil, err }
	}

	return this.ReqClient.Do(this)
}
