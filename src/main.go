package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
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
	r         *bufio.Reader
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

func (cr *ChunkedReader) Read(p []byte) (int, error) {
	if cr.remaining == 0 {
		line, err := cr.r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		cleanStr := strings.TrimSpace(line)
		val, err := strconv.ParseInt(cleanStr, 16, 64)
		if err != nil {
			return 0, err
		}
		if val == 0 {
			// Read trailing \r\n after the final 0-length chunk
			_, err := cr.r.ReadString('\n')
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

	// Consume trailing \r\n after chunk data
	if cr.remaining == 0 {
		_, err := cr.r.ReadString('\n')
		if err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (cw *ChunkedWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	_, err := fmt.Fprintf(cw.w, "%x\r\n", len(p))
	if err != nil {
		return 0, err
	}
	n, err := cw.w.Write(p)
	if err != nil {
		return n, err
	}
	_, err = cw.w.Write([]byte("\r\n"))
	return n, err
}

func (cw *ChunkedWriter) Close() error {
	_, err := cw.w.Write([]byte("0\r\n\r\n"))
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

	count := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return HTTPReq{}, fmt.Errorf("something went wrong: %w", err)
		}

		headerBytes += len(line)
		if headerBytes > 8192 {
			return HTTPReq{}, fmt.Errorf("headers too large")
		}

		// Blank line signals end of headers
		if line == "\r\n" {
			httpReq.Headers = headerArr

			if httpReq.Method == "GET" || httpReq.Method == "HEAD" {
				return httpReq, nil
			}

			for _, h := range headerArr {
				if h.Name == "content-length" {
					contentLength, err := strconv.Atoi(h.Value)
					if err != nil {
						return HTTPReq{}, fmt.Errorf("invalid content-length: %w", err)
					}
					httpReq.Body = &LimitedBodyReader{
						buf:       reader,
						remaining: contentLength,
					}
					return httpReq, nil
				} else if h.Name == "transfer-encoding" && h.Value == "chunked" {
					httpReq.Body = &ChunkedReader{
						r:         reader,
						remaining: 0,
					}
					return httpReq, nil
				} 
			}

			// No body indicated — return with nil Body
			return httpReq, nil
		}

		// Parse request line
		if count == 0 {
			lineData := strings.Split(line, " ")
			if len(lineData) < 3 {
				return HTTPReq{}, fmt.Errorf("malformed request line: %q", line)
			}
			httpReq.Method = strings.TrimSpace(lineData[0])
			httpReq.URI = strings.TrimSpace(lineData[1])
			httpReq.Version = strings.TrimSpace(lineData[2])
		} else {
			// Parse header line
			lineData := strings.SplitN(line, ":", 2)
			if len(lineData) < 2 {
				return HTTPReq{}, fmt.Errorf("malformed header line: %q", line)
			}
			myHeader := Header{
				Name:  strings.ToLower(strings.TrimSpace(lineData[0])),
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
		if strings.EqualFold(h.Name, "content-length") && h.Value != "" {
			isContentLength = true
		}
		headerLine := h.Name + ": " + h.Value + "\r\n"
		writer.Write([]byte(headerLine))
	}

	if !isContentLength {
		writer.Write([]byte("Transfer-Encoding: chunked\r\n"))
	}
	writer.Write([]byte("\r\n"))

	if isContentLength {
		_, err := io.Copy(writer, bodyReader)
		if err != nil {
			return fmt.Errorf("an error occurred: %w", err)
		}
	} else {
		cw := ChunkedWriter{w: writer}
		_, err := io.Copy(&cw, bodyReader)
		if err != nil {
			return fmt.Errorf("an error occurred: %w", err)
		}
		cw.Close()
	}

	writer.Flush()
	return nil
}

func sanitizeResource(URI string) (string, error) {
	if len(URI) == 0 {
		return "", fmt.Errorf("please enter a valid URI")
	}

	_, rest, _ := strings.Cut(URI, "/")
	_, remainder, found := strings.Cut(rest, "/")
	remainder = filepath.Clean(remainder)

	if !found {
		return "", fmt.Errorf("file not valid")
	}

	final := filepath.Join("static", remainder)
	if !strings.HasPrefix(final, "static/") {
		return "", fmt.Errorf("file not valid")
	}
	return final, nil
}

// drainBody safely discards any remaining request body bytes.
func drainBody(body io.Reader) {
	if body != nil {
		io.Copy(io.Discard, body)
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
			for {
				req, err := parseHTTPRequest(reader)
				if errors.Is(err, io.EOF) {
					return
				} else if err != nil {
					fmt.Println("parse error:", err)
					httpResponseWriter(conn, 400, []Header{}, strings.NewReader(""))
					return
				}

				sanitizedResource, err := sanitizeResource(req.URI)
				if err != nil {
					httpResponseWriter(conn, 404, []Header{}, strings.NewReader("Not Found"))
					drainBody(req.Body)
					continue
				}

				data, err := os.Open(sanitizedResource)
				if err != nil {
					httpResponseWriter(conn, 404, []Header{}, strings.NewReader("Not Found"))
					drainBody(req.Body)
					continue
				}

				dataInfo, err := data.Stat()
				if err != nil {
					httpResponseWriter(conn, 404, []Header{}, strings.NewReader("Not Found"))
					drainBody(req.Body)
					data.Close()
					continue
				}
				for header := range req.Headers{
					if req.Headers[header].Name == "range" && req.Headers[header].Value != ""{
					rangeBytes := strings.TrimSpace(req.Headers[header].Value)
					
					if strings.HasPrefix(rangeBytes,"bytes"){
						_, after,found := strings.Cut(rangeBytes, "=")
						if found {
							before,after,found := strings.Cut(after, "-")
							if found && before != "" && after != ""{
								intBefore, err := strconv.Atoi(before)
								if err != nil{
									var header Header
									header.Name = "Content-Range"
									header.Value = ""
									httpResponseWriter(conn, 500,  []Header{header}, strings.NewReader("Internal Server Error"))
									continue
								}
								intAfter, err := strconv.Atoi(after)
								if err != nil {
									httpResponseWriter(conn, 500, []Header{}, strings.NewReader("Internal Server Error"))
									continue
								}
								fmt.Printf("", intBefore, intAfter)
								
							}else if found && before != "" && after == ""{
								fmt.Print("Case 2")
							}else if found && before == "" && after != ""{
								fmt.Print("Case 3")
							}
						}
					}
				}
				}

				header := Header{
					Name:  "Content-Length",
					Value: strconv.FormatInt(dataInfo.Size(), 10),
				}
				httpResponseWriter(conn, 200, []Header{header}, data)
				drainBody(req.Body)
				data.Close()

				if req.Version == "HTTP/1.0" {
					break
				}
			}
		}(conn)
	}
}