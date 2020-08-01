package reqtify

import (
	"github.com/thewug/reqtify/test"
	"testing"
	"encoding/base64"
	"sync"
	"time"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"strings"
	"fmt"
	"reflect"
	"errors"
)

func TestNewReqtifier(t *testing.T) {
	lc_canary := false

	root := "https://example.root"
	ticker := time.NewTicker(time.Millisecond * 500)
	client := http.Client{}
	agent := "test"

	x := New(root, ticker, &client, func(Request) (error) { lc_canary = true; return nil }, agent)
	reqimpl := x.(*ReqtifierImpl)

	reqimpl.LastChance(nil)
	if !lc_canary {
		t.Error("Last Chance canary still alive! (provided function not called?)")
	}

	if reqimpl.RateLimiter != ticker {
		t.Error("Ticker didn't make it into ReqtifierImpl")
	}

	if reqimpl.HttpClient != &client {
		t.Error("HTTPClient didn't make it into ReqtifierImpl")
	}

	if reqimpl.AgentName != agent {
		t.Error("user agent didn't make it into ReqtifierImpl")
	}

	if reqimpl.Root != root {
		t.Error("root didn't make it into ReqtifierImpl")
	}

	x = New(root, ticker, nil, func(Request) (error) { lc_canary = true; return nil }, agent)
	reqimpl = x.(*ReqtifierImpl)

	if reqimpl.HttpClient == nil {
		t.Error("failed to create default HTTPClient")
	}
}

type TestStruct struct {
	Test string `json:"test_field"`
}

func TestReqtifierImplDo(t *testing.T) {
	var http_mock_client test.MockHttpClient
	examiner := http_mock_client.Examine()
	var err error
	var resp *http.Response
	var wg sync.WaitGroup

	reqtImpl := ReqtifierImpl{
		Root: "https://this.is.a.test",
		RateLimiter: nil,
		HttpClient: &http_mock_client,
		AgentName: "test",
	}

	cookie1 := http.Cookie{
		Name: "testcookie",
		Value: "testval",
	}

	hkey := "testheader"
	hval := "testval"

	body := cachedBody{
		body: []byte("test"),
		mimetype: "testmime",
	}
	emptyBody := cachedBody{
		body: nil,
		mimetype: "",
	}

	json_prototype := TestStruct{
		Test: "thisisatest",
	}
	json_string := func(b []byte, e error)(string){return string(b)}(json.Marshal(json_prototype))

	// iteration 1. Test a straightforward case
	// using a GET request, returning json into a marshaller.
	// request succeeds.
	var test_json TestStruct

	req := RequestImpl{
		URLPath: "/test",
		Verb: GET,
		BasicUser: "test",
		BasicPassword: "testpw",
		ReqClient: &reqtImpl,
		Headers: map[string]string{hkey: hval},
		Cookies: []*http.Cookie{&cookie1},
		Response: []ResponseUnmarshaller{FromJSON(&test_json)},
		body: &emptyBody,
	}

	wg.Add(1)
	go func() {
		resp, err = reqtImpl.Do(&req)
		wg.Done()
	}()

	request := <- examiner.Requests

	// examine the request to make sure it's what it should be
	if request.URL.String() != req.URL() { t.Errorf("URL Mismatch: got %s, expected %s", request.URL.String(), req.URL()) }
	if request.Method != string(GET) { t.Errorf("Method Mismatch: got %s, expected %s", request.Method, req.Verb) }

	// every header that's in the reqtify request should be in the http.request.
	// not true in general, but true in this instance.
	for k, _ := range req.Headers {
		if !reflect.DeepEqual(req.Headers[k], request.Header.Get(k)) { t.Errorf("Header Mismatch for %s: got %+v, expected %+v", k, request.Header.Get(k), req.Headers[k]) }
	}

	// it should also have a cookies header
	cookie_header := request.Header.Get("cookie")
	if cookie_header != cookie1.String() {
		t.Errorf("Cookie Mismatch: got %s, expected %s", cookie_header, cookie1.String())
	}

	// it should also have a basic auth header
	auth_header := request.Header.Get("authorization")
	expected := fmt.Sprintf("Basic %s", base64.URLEncoding.EncodeToString([]byte(req.BasicUser + ":" + req.BasicPassword)))
	if auth_header != expected {
		t.Errorf("Auth Mismatch: got %s, expected %s", auth_header, expected)
	}

	if request.Body != nil {
		t.Errorf("Body Mismatch: got something, expected nil")
	}

	// answer the request and wait for Do() to return, populating resp and err
	examiner.Responses <- test.ResponseAndError{
		Response: &http.Response{
			Body: ioutil.NopCloser(strings.NewReader(json_string)),
		},
		Error: nil,
	}
	wg.Wait()

	if resp == nil || err != nil {
		t.Errorf("Failure Mismatch: should have succeeded but didn't")
	}

	if test_json != json_prototype { t.Errorf("Response Marshaller Mismatch: got %+v, expected %+v", test_json, json_prototype) }

	// iteration 2. Test a PATCH request with a body
	// that fails.
	req2 := RequestImpl{
		URLPath: "/test2",
		Verb: PATCH,
		ReqClient: &reqtImpl,
		body: &body,
	}

	wg.Add(1)
	go func() {
		resp, err = reqtImpl.Do(&req2)
		wg.Done()
	}()

	request2 := <- examiner.Requests

	if request2.URL.String() != req2.URL() { t.Errorf("URL Mismatch: got %s, expected %s", request2.URL.String(), req2.URL()) }
	if request2.Method != string(PATCH) { t.Errorf("Method Mismatch: got %s, expected %s", request2.Method, PATCH) }

	// it have a content type header
	content_header := request2.Header.Get("Content-Type")
	if content_header != body.mimetype {
		t.Errorf("Auth Mismatch: got %s, expected %s", content_header, body.mimetype)
	}

	if request2.Body == nil {
		t.Errorf("Body Mismatch: got nil, expected non-nil")
	} else {
		body_text := func(b []byte, e error)(string){return string(b)}(ioutil.ReadAll(request2.Body))
		if body_text != string(body.body) {
			t.Errorf("Body Mismatch: got %s, expected %s", body_text, body.mimetype)
		}
	}

	// answer the request and wait for Do() to return, populating resp and err
	examiner.Responses <- test.ResponseAndError{
		Response: &http.Response{
		},
		Error: errors.New("error"),
	}
	wg.Wait()

	if resp != nil || err == nil || err.Error() != "error" {
		t.Errorf("Failure Mismatch: should have failed but didn't")
	}

	if t.Failed() {
		t.Logf("\nreq 1: %+v\nreq 2: %+v\n", req, req2)
	}
}
