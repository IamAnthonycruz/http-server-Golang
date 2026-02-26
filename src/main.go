package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)
type HTTPReq struct {
    Method string
    URI string
    Version string
    Headers []Header
    Body io.Reader
}
type Header struct {
    Name string
    Value string
}
type LimitedBodyReader struct {
    buf *bufio.Reader
    remaining int
}

func (r *LimitedBodyReader)Read(p []byte)(int, error){
    if r.remaining == 0{
        return 0, io.EOF
    }else{
        bytesIWant := min(len(p), r.remaining)
        n,err := r.buf.Read(p[:bytesIWant])
        if err != nil {
            return 0, fmt.Errorf("An error occurred %w", err)
        }
        r.remaining -= n
        return n, nil

    }
}
func parseHTTPRequest(reader *bufio.Reader) (HTTPReq, error){
    var httpReq HTTPReq
    var headerArr []Header
    var headerBytes int
    var contentLength int
    var limitedBodyReader LimitedBodyReader
    isHeaderComplete := false
    count := 0
    for {
        bytes, err := reader.ReadString('\n')
        
        line:= bytes
        if err != nil {
            return HTTPReq{}, fmt.Errorf("Something went wrong %w", err)
        }
        if isHeaderComplete == false {
            headerBytes += len(bytes)
            if headerBytes > 8192 {
                return HTTPReq{}, fmt.Errorf("Buffer is too large")
                
            }
            if bytes == "\r\n" {
                isHeaderComplete = true
                continue
            }
            if count == 0 {
                lineData := strings.Split(string(line), " ")
                httpReq.Method = strings.TrimSpace(lineData[0])
                httpReq.URI = strings.TrimSpace(lineData[1])
                httpReq.Version = strings.TrimSpace(lineData[2])
            }else{
                lineData := strings.SplitN(string(line), ":", 2)
            myHeader := Header{
            Name: strings.ToLower(strings.TrimSpace(lineData[0])),
            Value: strings.TrimSpace(lineData[1]),
            }
            headerArr = append(headerArr, myHeader)
        }
        count++
        
        } else if(isHeaderComplete == true){
            httpReq.Headers = headerArr
            if httpReq.Method == "GET" || httpReq.Method == "HEAD"{
                return httpReq, nil
            }
            for header := range headerArr{
                if headerArr[header].Name == "content-length"{
                    contentLength, err = strconv.Atoi(headerArr[header].Value)
                    if err != nil{
                        return HTTPReq{}, fmt.Errorf("An error occurred %w", err)
                    }
                }
            }
            limitedBodyReader.buf = reader
            limitedBodyReader.remaining = contentLength
            httpReq.Body = &limitedBodyReader
            break
        }
    
    }
    
    return  httpReq, nil
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
            continue
        }
        go parseHTTPRequest(bufio.NewReader(conn))
        
    }
}