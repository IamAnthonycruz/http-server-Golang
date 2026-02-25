package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)
type HTTPReq struct {
    Method string
    URI string
    Version string
    Headers []Header
}
type Header struct {
    Name string
    Value string
}
func parseHTTPRequest(reader *bufio.Reader) (HTTPReq, error){
    var httpReq HTTPReq
    var headerArr []Header
    var totalBytes int
    count := 0
    for {
        b, err := reader.ReadString('\n')
        totalBytes += len(b)
        if err != nil {
            return HTTPReq{}, fmt.Errorf("Something went wrong %w", err)
        }
        if totalBytes > 8192 {
            return HTTPReq{}, fmt.Errorf("Buffer is too large")
        }
        if b == "\r\n" {
            break
        }
        line:= b

        if count == 0 {
            lineData := strings.Split(string(line), " ")
            httpReq.Method = strings.TrimSpace(lineData[0])
            httpReq.URI = strings.TrimSpace(lineData[1])
            httpReq.Version = strings.TrimSpace(lineData[2])
        }else{
            lineData := strings.SplitN(string(line), ":", 2)
            myHeader := Header{
            Name: strings.TrimSpace(lineData[0]),
            Value: strings.TrimSpace(lineData[1]),
            }
            headerArr = append(headerArr, myHeader)
        }
        count++
    } 
    httpReq.Headers = headerArr
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