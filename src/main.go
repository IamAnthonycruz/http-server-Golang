package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

type HTTPReq struct {
	Method  string
	URI     string
	Version string
	Headers []Header
	Body    io.Reader
}
type ChunkedWriter struct {
	w io.Writer
}
type ChunkedReader struct {
	r *bufio.Reader
	remaining int
}
type Header struct {
	Name  string
	Value string
}

type LimitedBodyReader struct {
	buf       *bufio.Reader
	remaining int
}
func (cr *ChunkedReader) Read(p []byte)(int, error){
	if cr.remaining == 0 {
		line, err := cr.r.ReadString('\n')
		if err != nil{
			return 0, err
		}
		cleanStr := strings.TrimSpace(line)
		val, err := strconv.ParseInt(cleanStr, 16, 64)
		if err != nil {
			return 0, err
		}
		if val == 0 {
			_,err := cr.r.ReadString('\n')
			if err != nil {
				return 0, err
			}
			
					return 0, io.EOF
			
			
		}
		cr.remaining = int(val)

	}
	bytesIWant := min(len(p), cr.remaining)
	n, err := cr.r.Read(p[:bytesIWant])
	if err != nil {
		return 0, fmt.Errorf("an error occurred: %w", err)
	}
	cr.remaining -= n
	if cr.remaining == 0 {
		_, err := cr.r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		
	}
	return n, nil
	
}
func (cw *ChunkedWriter) Write(p []byte) (int, error){
	if len(p) == 0 {
		return 0, nil
	}
	_, err := fmt.Fprintf(cw.w, "%x\r\n", len(p))
	if err != nil {return 0, err}
	n, err := cw.w.Write(p)
	if err != nil {return n, err}
	_,err = cw.w.Write([]byte("\r\n"))
	return n,err
}
func (cw *ChunkedWriter) Close() error {
	_,err := cw.w.Write([]byte("0\r\n\r\n"))
	return err
}

func (r *LimitedBodyReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	bytesIWant := min(len(p), r.remaining)
	n, err := r.buf.Read(p[:bytesIWant])
	if err != nil {
		return 0, fmt.Errorf("an error occurred: %w", err)
	}
	r.remaining -= n
	return n, nil
}

func parseHTTPRequest(reader *bufio.Reader) (HTTPReq, error) {
	var httpReq HTTPReq
	var headerArr []Header
	var headerBytes int
	var contentLength int
	
	count := 0

	for {
		bytes, err := reader.ReadString('\n')
		if err != nil {
			return HTTPReq{}, fmt.Errorf("something went wrong: %w", err)
		}

		headerBytes += len(bytes)
		if headerBytes > 8192 {
			return HTTPReq{}, fmt.Errorf("headers too large")
		}

		// Blank line signals end of headers
		if bytes == "\r\n" {
			httpReq.Headers = headerArr

			if httpReq.Method == "GET" || httpReq.Method == "HEAD" {
				return httpReq, nil
			}

			for _, h := range headerArr {
				if h.Name == "content-length" {
					contentLength, err = strconv.Atoi(h.Value)
					if err != nil {
						return HTTPReq{}, fmt.Errorf("invalid content-length: %w", err)
					}
					httpReq.Body = &LimitedBodyReader{
					buf:       reader,
					remaining: contentLength,
				}}else if h.Value == "chunked"{
					
					httpReq.Body = &ChunkedReader{
					r: reader,
					remaining: 0,
					}
				}
			}
			
		}

		// Parse request line
		if count == 0 {
			lineData := strings.Split(bytes, " ")
			if len(lineData) < 3 {
				return HTTPReq{}, fmt.Errorf("malformed request line: %q", bytes)
			}
			httpReq.Method = strings.TrimSpace(lineData[0])
			httpReq.URI = strings.TrimSpace(lineData[1])
			httpReq.Version = strings.TrimSpace(lineData[2])
		} else {
			// Parse header line
			lineData := strings.SplitN(bytes, ":", 2)
			if len(lineData) < 2 {
				return HTTPReq{}, fmt.Errorf("malformed header line: %q", bytes)
			}
			myHeader := Header{
				Name:  strings.TrimSpace(lineData[0]),
				Value: strings.TrimSpace(lineData[1]),
			}
			headerArr = append(headerArr, myHeader)
		}

		count++
	}
}

func httpResponseWriter(conn net.Conn, statusCode int, headers []Header, bodyReader io.Reader) error {
	writer := bufio.NewWriter(conn)
	isContentLength := false
	responseCodeMap := map[int]string{
		200: "OK",
		201: "Created",
		204: "No Content",
		400: "Bad Request",
		401: "Unauthorized",
		403: "Forbidden",
		404: "Not Found",
		408: "Request Timeout",
		499: "Client Closed Request",
		500: "Internal Server Error",
	}

	reason, ok := responseCodeMap[statusCode]
	if !ok {
		return errors.New("status code is invalid")
	}

	startline := fmt.Sprintf("HTTP/1.1 %d %s\r\n", statusCode, reason)
	writer.Write([]byte(startline))
	
	for _, h := range headers {
		if h.Name == "content-length" && h.Value != "" {
			isContentLength = true
		}
		headerLine := h.Name + ": " + h.Value + "\r\n"
		
		writer.Write([]byte(headerLine))
	}
	if isContentLength == false {
		headerLine := "Transfer-Encoding"+ ": "+"chunked" + "\r\n"
		writer.Write([]byte(headerLine))
	}
	writer.Write([]byte("\r\n"))

	if isContentLength == true{
		_, err := io.Copy(writer, bodyReader)
		if err != nil {
			return fmt.Errorf("an error occurred: %w", err)
		}

	}else{
		var chunkWriter ChunkedWriter
		chunkWriter.w = writer
		_, err := io.Copy(&chunkWriter, bodyReader)
		if err != nil {
			return fmt.Errorf("an error occured: %w", err)
		}
		chunkWriter.Close()
	}
	
	writer.Flush()
	return nil
}

func sanitizeResource(URI string)(cleanURI string, err error){
	if len(URI) == 0 {
		return "", fmt.Errorf("Please enter a valid URI")
	}
	_,rest,_ := strings.Cut(URI, "/")
	_, remainder, found := strings.Cut(rest,"/")
	remainder = filepath.Clean(remainder)
	
	if found {
		final := filepath.Join("static", remainder)
		if strings.HasPrefix(final, "static/"){
		return final, nil
		}else {
			return "", fmt.Errorf("File not valid")
		}
	} else {
		return "", fmt.Errorf("File not valid")
	}

}

func main() {
	ln, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on :8080")

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			return
		}

		go func(conn net.Conn) {
            defer conn.Close()
			reader := bufio.NewReader(conn)
            for{
				req, err := parseHTTPRequest(reader)
				if errors.Is(err, io.EOF) {
					return
				}else if err != nil {
					fmt.Println("parse error:", err)
					httpResponseWriter(conn, 400, []Header{}, strings.NewReader(""))
					return
				} /*
				resource := req.URI
				sanatizedresouce = func sanitizeResource(resource)
				if sanatizedresource in some datastructure containing our uris
					stream our body back
				else throw some resource not found error and respond with 404
					*/
				
				if req.Body == nil{
                    req.Body = strings.NewReader("")
                }
				header := Header{Name: "Content-Length", Value: "11"}
                httpResponseWriter(conn, 200, []Header{header}, strings.NewReader("Hello world"))
                io.Copy(io.Discard, req.Body)
                if req.Version == "HTTP/1.0" {
                    break
                }
        }
		}(conn)
	}
}