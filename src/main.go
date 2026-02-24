package main

import (
	"fmt"
	"net"
	"strings"
)
func httpRequestParser(conn net.Conn){
    defer conn.Close()
    buf := make([]byte, 8192)
    var fullRequest []byte
    for {
        n, err := conn.Read(buf)
        if err != nil {
            break
        }
        fullRequest = append(fullRequest, buf[:n]...)
        if strings.Contains(string(fullRequest), "\r\n\r\n") {
            header := fullRequest
            var line []byte
            for i:=0; i < len(header); i++ {
                line = append(line, header[i])
                if len(line) >=2 && line[len(line)-2] == '\r' && line[len(line)-1] == '\n' {
                    fmt.Println("End of line reached")
                    fmt.Print(string(line))

                    line = nil
                }
            }
            
        }
        

    
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
            continue
        }
        go httpRequestParser(conn)
         // each client gets its own goroutine
    }
}