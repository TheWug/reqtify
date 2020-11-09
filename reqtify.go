package reqtify

import (
	"bytes"
	"time"
	"io"
	"net/http"
	"net/url"
	"io/ioutil"
	"encoding/json"
	"encoding/xml"
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

type Reqtifier interface {
	New(string) (Request)
}

type Request interface {
	Do() (*http.Response, error)

	Method(v HttpVerb) (Request)
	Path(path string) (Request)
	Header(key, value string) (Request)
	Cookie(c *http.Cookie) (Request)
	BasicAuthentication(user, password string) (Request)
	Multipart() (Request)

	Arg(key string, value interface{}) (Request)
	URLArg(key string, value interface{}) (Request)
	FormArg(key string, value interface{}) (Request)
	FileArg(key, filename string, data io.Reader) (Request)

	ArgDefault(key string, value, def interface{}) (Request)
	URLArgDefault(key string, value, def interface{}) (Request)
	FormArgDefault(key string, value, def interface{}) (Request)

	Into(into ResponseUnmarshaller) (Request)
	JSONInto(into interface{}) (Request)
	XMLInto(into interface{}) (Request)

	DebugPrint() (Request)
	GetBody() (io.Reader, string)

	Target() (string)
	URL() (string)
	GetPath() (string)
}

type HttpRequester interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (resp *http.Response, err error)
	Head(url string) (resp *http.Response, err error)
	Post(url, contentType string, body io.Reader) (resp *http.Response, err error)
	PostForm(url string, data url.Values) (resp *http.Response, err error)
}

type ReqtifierImpl struct {
	Root         string
	RateLimiter *time.Ticker
	HttpClient   HttpRequester
	LastChance   func(Request) error
	AgentName    string
}

type ResponseUnmarshaller interface {
	Unmarshal([]byte) error
}

type JSONUnmarshaller struct {
	output_value interface{}
}

func (this JSONUnmarshaller) Unmarshal(body []byte) error {
	return json.Unmarshal(body, this.output_value)
}

func FromJSON(output_value interface{}) ResponseUnmarshaller {
	return JSONUnmarshaller{output_value: output_value}
}

type XMLUnmarshaller struct {
	output_value interface{}
}

func (this XMLUnmarshaller) Unmarshal(body []byte) error {
	return xml.Unmarshal(body, this.output_value)
}

func FromXML(output_value interface{}) ResponseUnmarshaller {
	return XMLUnmarshaller{output_value: output_value}
}

type cachedBody struct {
	body   []byte
	mimetype string
}

type RequestImpl struct {
	URLPath        string
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

	Response     []ResponseUnmarshaller

	ReqClient     *ReqtifierImpl

	body          *cachedBody
}

func New(root string, rl *time.Ticker, client *http.Client, lc func(Request) (error), agent string) (Reqtifier) {
	r := ReqtifierImpl{
		Root: root,
		RateLimiter: rl,
		HttpClient: client,
		LastChance: lc,
		AgentName: agent,
	}

	if client == nil {
		r.HttpClient = &http.Client{Transport: &http.Transport{} }
	}

	return &r
}

func (this *ReqtifierImpl) Do(req *RequestImpl) (*http.Response, error) {
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

	// try to close any closable formfiles passed to us
	for _, list := range req.FormFiles {
		for _, file := range list {
			closer := file.Data.(io.ReadCloser)
			if closer != nil {
				closer.Close()
			}
		}
	}

	// Packing into response, if we have one
	if len(req.Response)!= 0 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()

		resp.Body = ioutil.NopCloser(bytes.NewReader(body))
		for _, response := range req.Response {
			e := response.Unmarshal(body)
			if err != nil {
				err = e
			}
		}
	}

	// OK, though err might not be nil if there is a marshalling error
	return resp, err
}

func (this *ReqtifierImpl) New(endpoint string) (Request) {
	return &RequestImpl{
		URLPath: endpoint,
		Verb: GET,
		QueryParams: url.Values{},
		FormParams: url.Values{},
		AutoParams: url.Values{},
		FormFiles: make(map[string][]FormFile),
		Headers: make(map[string]string),
		ReqClient: this,
	}
}

func (this *RequestImpl) GetBody() (io.Reader, string) {
	if this.body != nil {
		// if we've resolved the body already, re-use it.
		if this.body.body == nil {
			return nil, ""
		}

		return &readOnlyReader{buffer: this.body.body}, this.body.mimetype
	} else if this.ForceMultipart || len(this.FormFiles) != 0 {
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

func (this *RequestImpl) Path(path string) (Request) {
	this.URLPath = path
	return this
}

func (this *RequestImpl) Method(v HttpVerb) (Request) {
	this.Verb = v
	return this
}

func (this *RequestImpl) Into(into ResponseUnmarshaller) (Request) {
	this.Response = append(this.Response, into)
	return this
}

func (this *RequestImpl) JSONInto(into interface{}) (Request) {
	this.Response = append(this.Response, FromJSON(into))
	return this
}

func (this *RequestImpl) XMLInto(into interface{}) (Request) {
	this.Response = append(this.Response, FromXML(into))
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
	case rune:
		return string(x), true
	case *rune:
		if x == nil { return "", false }
		return string(*x), true
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

// for Arg, URLArg, and FormArg, the value will be converted to a string and included among the keys.
// the following types are supported, and have the following behavior:
//   string: included verbatim.
//   int, int64, rune, float32, float64: converted to string and included verbatim.
//   bool: converted to "true" or "false".
//   pointers to any of the above: omitted if nil, otherwise dereferenced, converted to string, and included.
//   fmt.Stringer: omitted if nil, otherwise .String() is called, and output included verbatim.
//   anything else: panic is called.

func (this *RequestImpl) Arg(key string, value interface{}) (Request) {
	return this.argDefaultHelper(key, value, nil, this.AutoParams)
}

func (this *RequestImpl) URLArg(key string, value interface{}) (Request) {
	return this.argDefaultHelper(key, value, nil, this.QueryParams)
}

func (this *RequestImpl) FormArg(key string, value interface{}) (Request) {
	return this.argDefaultHelper(key, value, nil, this.FormParams)
}

func (this *RequestImpl) FileArg(key, filename string, data io.Reader) (Request) {
	this.FormFiles[key] = append(this.FormFiles[key], FormFile{Name: filename, Data: data})
	return this
}

// for ArgDefault, URLArgDefault, and FormArgDefault, in addition to omitting the argument
// if nil is passed (see above), it is also omitted if it matches a provided default value,
// or if the converted string matches that value (so 3 will match a default of either 3, or "3")

func (this *RequestImpl) ArgDefault(key string, value, def interface{}) (Request) {
	return this.argDefaultHelper(key, value, def, this.AutoParams)
}

func (this *RequestImpl) URLArgDefault(key string, value, def interface{}) (Request) {
	return this.argDefaultHelper(key, value, def, this.QueryParams)
}

func (this *RequestImpl) FormArgDefault(key string, value, def interface{}) (Request) {
	return this.argDefaultHelper(key, value, def, this.FormParams)
}

func (this *RequestImpl) argDefaultHelper(key string, value, def interface{}, values url.Values) (Request) {
	if value != def {
		if str, present := stringify(value); present && str != def {
			values.Add(key, str)
		}
	}
	return this
}

func (this *RequestImpl) Header(key, value string) (Request) {
	this.Headers[strings.ToLower(key)] = value
	return this
}

func (this *RequestImpl) Cookie(c *http.Cookie) (Request) {
	this.Cookies = append(this.Cookies, c)
	return this
}

func (this *RequestImpl) BasicAuthentication(user, password string) (Request) {
	this.BasicUser = user
	this.BasicPassword = password
	return this
}

func (this *RequestImpl) Multipart() (Request) {
	this.ForceMultipart = true
	return this
}

func (this *RequestImpl) Target() (string) {
	return this.ReqClient.Root + this.URLPath
}

func (this *RequestImpl) GetPath() (string) {
	return this.URLPath
}

func (this *RequestImpl) URL() (string) {
	callURL := this.Target()
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

// you should not call any other functions which mutate the request object
// after calling this function. It will resolve the request body to a byte
// array and store it, and GetBody() will return the stored one later, rather
// than re-resolving it. This is done because the body may be constructed from
// external io.Readers which can't be seeked or re-read, only read once.
func (this *RequestImpl) DebugPrint() (Request) {
	reader, mimetype := this.GetBody()
	body, err := ioutil.ReadAll(reader)
	if err != nil { panic("Error reading request body: " + err.Error()) }

	this.body = &cachedBody{
		body: body,
		mimetype: mimetype,
	}

	log.Printf("Request URL: %s\nUser agent: %s\nOther request headers: %+v\nRequest body:\n%s\n\n", this.URL(), this.ReqClient.AgentName, this.Headers, string(body))
	return this
}

// Call this function to execute the call.
// it can return a nil response if an error occurs.
func (this *RequestImpl) Do() (*http.Response, error) {
	if len(this.ReqClient.AgentName) != 0 {
	        this.Header("User-Agent", this.ReqClient.AgentName)
	}

	if this.ReqClient.LastChance != nil {
		err := this.ReqClient.LastChance(this)
		if err != nil { return nil, err }
	}

	return this.ReqClient.Do(this)
}
