package reqtify

import (
	"io"
	"math/rand"
	"strings"
	"fmt"
	"bytes"
)

/*
   This file duplicates functionality provided by the "mime/multipart" system library,
   with one crucial difference. the system library uses io.Writer, which can result in
   added latency when dealing with large files, remote network resources, or other such
   edge cases due to accidental buffering and copying. This implementation is io.Reader
   based, which allows it to produce streaming responses and achieve high performance
   and low latency even under the most adverse of conditions.

   Use:
	1. create object
	2. call addParam() or addFileParam() as needed
	3. call close()
	4. call toReader() to create an io.Reader which emits the form's body
	5. call contentType() to fetch the correct content type for the form
*/

type multipartRequestBody struct {
	readerlist  []io.Reader
	boundary    []byte
	effBoundary []byte
}

var letters []byte = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._")

func (this *multipartRequestBody) randomBoundary() {
	this.effBoundary = []byte("------multipart")
	for i := 0; i < 32; i++ {
		this.effBoundary = append(this.effBoundary, letters[rand.Intn(len(letters))])
	}
	this.effBoundary = append(this.effBoundary, '-', '-')
	this.boundary = this.effBoundary[2:len(this.effBoundary)-2]
}

func (this *multipartRequestBody) boundaryReader() (io.Reader) {
	if this.boundary == nil { this.randomBoundary() }
	return &readOnlyReader{buffer: this.effBoundary[:len(this.effBoundary)-2]}
}

func (this *multipartRequestBody) endBoundaryReader() (io.Reader) {
	if this.boundary == nil { this.randomBoundary() }
	return &readOnlyReader{buffer: this.effBoundary}
}

func (this *multipartRequestBody) toReader() (io.Reader) {
	return io.MultiReader(this.readerlist...)
}

func (this *multipartRequestBody) contentType() (string) {
	if this.boundary == nil { this.randomBoundary() }
	return fmt.Sprintf("multipart/form-data; charset=utf-8; boundary=\"%s\"", this.boundary)
}

func (this *multipartRequestBody) addParam(key, value string) {
	this.readerlist = append(this.readerlist,
		this.boundaryReader(),
		bytes.NewBuffer([]byte(fmt.Sprintf("\r\nContent-Disposition: form-data; name=\"%s\"\r\n\r\n", escapeQuotes(key)))),
		bytes.NewBuffer([]byte(value)),
		bytes.NewBuffer([]byte("\r\n")),
	)
}

func (this *multipartRequestBody) addFileParam(key string, file FormFile) {
	this.readerlist = append(this.readerlist,
		this.boundaryReader(),
		bytes.NewBuffer([]byte(fmt.Sprintf("\r\nContent-Disposition: form-data; name=\"%s\"; filename=\"%s\"\r\nContent-Type: application/octet-stream\r\n\r\n", escapeQuotes(key), escapeQuotes(file.Name)))),
		file.Data,
		bytes.NewBuffer([]byte("\r\n")),
	)
}

func (this *multipartRequestBody) close() {
	this.readerlist = append(this.readerlist, this.endBoundaryReader())
}

// an io.Reader which reads from a read only buffer.
// multiple readOnlyBuffers can share the same buffer, unlike bytes.Buffer.
type readOnlyReader struct {
	buffer []byte
	offset int
}

func (this *readOnlyReader) Read(p []byte) (int, error) {
	copied := copy(p, this.buffer[this.offset:])
	this.offset += copied
	if this.offset == len(this.buffer) {
		return copied, io.EOF
	} else {
		return copied, nil
	}
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}
