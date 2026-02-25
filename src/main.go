package main

import (
	"bytes"
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
func parseHTTPRequest(conn net.Conn) HTTPReq{
    defer conn.Close()
    buf := make([]byte, 8192)
    var body []byte
    var fullRequest []byte
    var httpReq HTTPReq
    for {
        n, err := conn.Read(buf)
        if err != nil {
            break
        }
        fullRequest = append(fullRequest, buf[:n]...)
        if len(fullRequest) > 8192 {
            
            return HTTPReq{}
        }
        if strings.Contains(string(fullRequest), "\r\n\r\n") {
            idx := bytes.Index(fullRequest, []byte("\r\n\r\n"))
            header := fullRequest[:idx]
            body = append(body, fullRequest[idx+4:]...) //todo
            var headerArr []Header
            
            var line []byte

            count := 0
            for i:=0; i < len(header); i++ {
                line = append(line, header[i])
                if len(line) >=2 && line[len(line)-2] == '\r' && line[len(line)-1] == '\n' {
                    if count == 0{
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
                    line = nil
                }
            }
            httpReq.Headers=headerArr
            break
            
        }
        

    
    }
    return  httpReq
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
        go parseHTTPRequest(conn)
         // each client gets its own goroutine
    }
}