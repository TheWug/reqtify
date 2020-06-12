package reqtify

import (
	"time"
	"io"
	"net/http"
	"net/url"
	"io/ioutil"
	"encoding/json"
	"strings"
	"strconv"
	"fmt"
	"log"
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
	QueryParams    url.Values
	FormParams     url.Values
	AutoParams     url.Values
	FormFiles      map[string][]FormFile
	Headers        map[string]string
	BasicUser      string
	BasicPassword  string
	Cookies     []*http.Cookie
	ForceMultipart bool

	Response     []interface{}

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

	// override authentication with HTTP basic auth, if specified
	if (req.BasicUser != "" || req.BasicPassword != "") {
		r.SetBasicAuth(req.BasicUser, req.BasicPassword)
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
	if len(req.Response)!= 0 {
		body, err := ioutil.ReadAll(resp.Body)
		defer resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for i, response := range req.Response {
			e := json.Unmarshal(body, response)
			if i == 0 {
				err = e
			} else if err != nil {
				err = e
			}
		}
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
	this.Response = append(this.Response, into)
	return this
}

func stringify(i interface{}) (string, bool) {
	if i == nil { return "", false }

	switch x := i.(type) {
	case string:
		return x, true
	case *string:
		if x == nil { return "", false }
		return *x, true
	case int:
		return strconv.Itoa(x), true
	case *int:
		if x == nil { return "", false }
		return strconv.Itoa(*x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case *int64:
		if x == nil { return "", false }
		return strconv.FormatInt(*x, 10), true
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32), true
	case *float32:
		if x == nil { return "", false }
		return strconv.FormatFloat(float64(*x), 'f', -1, 32), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case *float64:
		if x == nil { return "", false }
		return strconv.FormatFloat(*x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	case *bool:
		if x == nil { return "", false }
		return strconv.FormatBool(*x), true
	default:
		if x, ok := i.(fmt.Stringer); ok {
			if x == nil { return "", false }
			return x.String(), true
		}
		panic(fmt.Sprintf("Couldn't convert value to string: %+v", i))
	}
}

// for Arg, URLArg, and FormArg, the value will always be converted to a string and included among the keys.
func (this *Request) Arg(key string, value interface{}) (*Request) {
	v, present := stringify(value)
	if present { this.AutoParams.Add(key, v) }
	return this
}

func (this *Request) URLArg(key string, value interface{}) (*Request) {
	v, present := stringify(value)
	if present { this.QueryParams.Add(key, v) }
	return this
}

func (this *Request) FormArg(key string, value interface{}) (*Request) {
	v, present := stringify(value)
	if present { this.FormParams.Add(key, v) }
	return this
}

func (this *Request) FileArg(key, filename string, data io.Reader) (*Request) {
	this.FormFiles[key] = append(this.FormFiles[key], FormFile{Name: filename, Data: data})
	return this
}

func (this *Request) ArgDefault(key string, value, def interface{}) (*Request) {
	if value != def {
		if str, present := stringify(value); present && str != def {
			this.AutoParams.Add(key, str)
		}
	}
	return this
}

func (this *Request) URLArgDefault(key string, value, def interface{}) (*Request) {
	if value != def {
		if str, present := stringify(value); present && str != def {
			this.QueryParams.Add(key, str)
		}
	}
	return this
}

func (this *Request) FormArgDefault(key string, value, def interface{}) (*Request) {
	if value != def {
		if str, present := stringify(value); present && str != def {
			this.FormParams.Add(key, str)
		}
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

func (this *Request) BasicAuthentication(user, password string) (*Request) {
	this.BasicUser = user
	this.BasicPassword = password
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

func (this *Request) DebugPrint() (*Request) {
	log.Printf("Request:\n%+v\n", *this)
	return this
}

// Call this function to execute the call.
// it can return a nil response if an error occurs.
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
